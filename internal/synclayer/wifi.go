package synclayer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// WifiSecurityPersonal is the personal (PSK / SAE) security configuration of an
// SSID.
type WifiSecurityPersonal struct {
	Password   string `json:"password"`
	Encryption string `json:"encryption,omitempty"`
	GroupKey   int    `json:"groupKey,omitempty"`

	// PlainPassword is the PSK recovered from the read envelope, and
	// PasswordRecovered reports whether that recovery succeeded. Both are
	// transient (never serialized): GetWifiSSID fills them so an update can
	// re-encode the password with the token in use instead of replaying an
	// envelope that may belong to an older session.
	PlainPassword     string `json:"-"`
	PasswordRecovered bool   `json:"-"`
}

// WifiSecurity is the security block of an SSID.
type WifiSecurity struct {
	Type     string                `json:"type"`
	Personal *WifiSecurityPersonal `json:"personal,omitempty"`
	MFP      string                `json:"mfp,omitempty"`
}

// WifiNumClient reports the client limit of an SSID.
type WifiNumClient struct {
	Max int `json:"max,omitempty"`
	Set int `json:"set"`
}

// WifiSSID is the detailed configuration of a single SSID on a band.
//
// Fields reported by the device but not managed by Terraform (for example
// macAddress) are carried through so that a read-modify-write round trip does
// not drop them.
type WifiSSID struct {
	Index         int           `json:"index"`
	Active        bool          `json:"active"`
	Type          string        `json:"type,omitempty"`
	Name          string        `json:"name"`
	MACAddress    string        `json:"macAddress,omitempty"`
	AccessControl bool          `json:"accessControl"`
	HiddenSSID    bool          `json:"hiddenSSID"`
	APIsolate     bool          `json:"APIsolate"`
	WebUIAccess   bool          `json:"webUIAccess"`
	InternetOnly  bool          `json:"internetOnly"`
	WMF           bool          `json:"wmf"`
	FT            bool          `json:"ft"`
	NumClient     WifiNumClient `json:"numClient"`
	Security      WifiSecurity  `json:"security"`
}

// WifiRadioChannel is the basic.channel block of a radio.
type WifiRadioChannel struct {
	Set string `json:"set"`
}

// WifiRadioBandwidth is the basic.bandwidth block of a radio.
type WifiRadioBandwidth struct {
	Set string `json:"set"`
}

// WifiRadioBasic is the basic block of a radio.
type WifiRadioBasic struct {
	WirelessMode string             `json:"wirelessMode"`
	Channel      WifiRadioChannel   `json:"channel"`
	OutputPower  string             `json:"outputPower,omitempty"`
	Bandwidth    WifiRadioBandwidth `json:"bandwidth"`
	Sideband     string             `json:"sideband,omitempty"`
}

// WifiRadio is the configuration of a single radio (band). The advanced and WMM
// blocks are kept as raw maps so that unknown firmware fields survive a
// read-modify-write round trip.
//
// The device also returns `channelList` and the `used` fields of
// `basic.channel` / `basic.bandwidth`; they are not carried through on write.
// Verified on VER-01.06.05-EA that a PUT without them is accepted (200) and the
// configuration is unchanged, so they are intentionally omitted.
type WifiRadio struct {
	Active   bool           `json:"active"`
	Basic    WifiRadioBasic `json:"basic"`
	Advanced map[string]any `json:"advanced,omitempty"`
	WMM      map[string]any `json:"wmm,omitempty"`
}

// ListWifiSSIDs returns the SSID summaries for a band (0 = 2.4GHz, 1 = 5GHz).
func (c *Client) ListWifiSSIDs(ctx context.Context, band int) ([]WifiSSID, error) {
	var ssids []WifiSSID
	token, err := c.requestWithToken(ctx, http.MethodGet, fmt.Sprintf("/wifi/%d/ssid", band), nil, &ssids)
	if err != nil {
		return nil, err
	}
	// Recover each PSK with the token that produced its envelope (see
	// GetWifiSSID).
	for i := range ssids {
		recoverWifiPassword(token, &ssids[i])
	}
	return ssids, nil
}

// GetWifiSSID returns the detailed configuration of one SSID. It returns
// (nil, nil) when the index does not exist.
func (c *Client) GetWifiSSID(ctx context.Context, band, index int) (*WifiSSID, error) {
	var ssid WifiSSID
	token, err := c.requestWithToken(ctx, http.MethodGet, fmt.Sprintf("/wifi/%d/ssid/%d", band, index), nil, &ssid)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	// Recover the PSK while we know the token that produced the envelope, so a
	// later update can re-encode it with the token in use instead of replaying a
	// possibly stale envelope.
	recoverWifiPassword(token, &ssid)

	return &ssid, nil
}

// recoverWifiPassword fills Personal.PlainPassword / PasswordRecovered using the
// token that produced the stored envelope. A failure is not fatal here (the read
// still succeeds); UpdateWifiSSID refuses to write an envelope it cannot
// attribute to a token.
func recoverWifiPassword(token string, ssid *WifiSSID) {
	p := ssid.Security.Personal
	if p == nil || p.Password == "" {
		return
	}
	plain, err := DecodeDDNSPassword(token, p.Password)
	if err != nil {
		return
	}
	p.PlainPassword = plain
	p.PasswordRecovered = true
}

// UpdateWifiSSID writes an SSID configuration.
//
// The device stores the PSK as an envelope keyed with the session token (the
// same scheme as DDNS) and keeps the envelope it is given. An explicit password
// is encoded with the token in use; otherwise the password recovered by
// GetWifiSSID is re-encoded with the token in use, so a re-authentication
// between the read and the write does not invalidate it. If a stored envelope
// exists but could not be recovered and no explicit password is given, the write
// is refused rather than replaying an envelope that may belong to an older
// session.
//
// Verified on VER-01.06.05-EA: replaying the read envelope keeps the PSK
// unchanged, while omitting the password field replaces it — so the password
// must always be sent.
func (c *Client) UpdateWifiSSID(ctx context.Context, band, index int, ssid WifiSSID, plainPassword string) error {
	path := fmt.Sprintf("/wifi/%d/ssid/%d", band, index)

	build := func(token string) (interface{}, error) {
		effective := ssid

		// Copy the personal block so the caller's struct is not mutated.
		if effective.Security.Personal != nil {
			personal := *effective.Security.Personal
			effective.Security.Personal = &personal
		}
		personal := effective.Security.Personal

		switch {
		case plainPassword != "":
			encoded, err := EncodeDDNSPassword(token, c.username, plainPassword)
			if err != nil {
				return nil, err
			}
			if personal == nil {
				personal = &WifiSecurityPersonal{}
				effective.Security.Personal = personal
			}
			personal.Password = encoded
		case personal != nil && personal.Password != "" && personal.PasswordRecovered:
			encoded, err := EncodeDDNSPassword(token, c.username, personal.PlainPassword)
			if err != nil {
				return nil, err
			}
			personal.Password = encoded
		case personal != nil && personal.Password != "":
			return nil, errors.New("cannot preserve the existing WiFi password: the stored value could not be decoded; set the password argument to replace it")
		}

		return effective, nil
	}

	return c.requestWithBody(ctx, http.MethodPut, path, build, nil)
}

// GetWifiRadio returns a band's radio configuration.
func (c *Client) GetWifiRadio(ctx context.Context, band int) (*WifiRadio, error) {
	var radio WifiRadio
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/wifi/%d/radio", band), nil, &radio); err != nil {
		return nil, err
	}
	return &radio, nil
}

// UpdateWifiRadio writes a band's radio configuration.
func (c *Client) UpdateWifiRadio(ctx context.Context, band int, radio WifiRadio) error {
	return c.request(ctx, http.MethodPut, fmt.Sprintf("/wifi/%d/radio", band), radio, nil)
}

// WifiAccessControlRule is one MAC address registered in an SSID's access
// control list. The device assigns the id and the name; the name is carried
// through so that a read-modify-write round trip does not lose it.
//
// The id is always serialized: a whole-list write is only faithful when every
// rule keeps the id the device assigned it, and id 0 is a valid id (verified on
// VER-01.06.05-EA).
type WifiAccessControlRule struct {
	ID         int    `json:"id"`
	Name       string `json:"name,omitempty"`
	MacAddress string `json:"macAddress"`
}

// wifiAccessControlRuleCreate is the body that registers a new rule. It carries
// no id: the device assigns one.
type wifiAccessControlRuleCreate struct {
	Name       string `json:"name,omitempty"`
	MacAddress string `json:"macAddress"`
}

// WifiAccessControl is the MAC access control list of one SSID.
type WifiAccessControl struct {
	Active bool                    `json:"active"`
	Allow  bool                    `json:"allow"`
	Rules  []WifiAccessControlRule `json:"rules"`
}

// GetWifiAccessControl returns the MAC access control configuration of one SSID.
// It returns (nil, nil) when the SSID index does not exist.
func (c *Client) GetWifiAccessControl(ctx context.Context, band, index int) (*WifiAccessControl, error) {
	var ac WifiAccessControl
	if err := c.request(ctx, http.MethodGet, fmt.Sprintf("/wifi/%d/ssid/%d/accessControl", band, index), nil, &ac); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &ac, nil
}

// UpdateWifiAccessControl writes the MAC access control configuration of one
// SSID.
//
// Every rule in Rules must carry the id the device gave it. Verified on
// VER-01.06.05-EA: a write that replays the rules with their ids keeps the list
// unchanged, but a write whose rules have no id does not replace the list
// faithfully — the device renumbers the entries and can drop one — so callers
// must reconcile the list with CreateWifiAccessControlRule and
// DeleteWifiAccessControlRule instead of sending a new one.
func (c *Client) UpdateWifiAccessControl(ctx context.Context, band, index int, ac WifiAccessControl) error {
	return c.request(ctx, http.MethodPut, fmt.Sprintf("/wifi/%d/ssid/%d/accessControl", band, index), ac, nil)
}

// CreateWifiAccessControlRule registers one MAC address in an SSID's access
// control list. The device assigns the rule's id.
func (c *Client) CreateWifiAccessControlRule(ctx context.Context, band, index int, rule WifiAccessControlRule) error {
	body := wifiAccessControlRuleCreate{Name: rule.Name, MacAddress: rule.MacAddress}
	return c.request(ctx, http.MethodPost, fmt.Sprintf("/wifi/%d/ssid/%d/accessControl", band, index), body, nil)
}

// DeleteWifiAccessControlRule removes one rule from an SSID's access control
// list, identified by the id the device assigned it.
func (c *Client) DeleteWifiAccessControlRule(ctx context.Context, band, index, ruleID int) error {
	return c.request(ctx, http.MethodDelete, fmt.Sprintf("/wifi/%d/ssid/%d/accessControl/%d", band, index, ruleID), nil, nil)
}

// WifiWPS mirrors GET /api/v1/wifi/wps. The WPS configuration is device-wide,
// not per band or per SSID.
type WifiWPS struct {
	Active bool   `json:"active"`
	PIN    string `json:"pin,omitempty"`

	// Status is the runtime pairing state reported by the device. It is
	// read-only and never sent back.
	Status string `json:"status,omitempty"`
}

// GetWifiWPS returns the device-wide WPS configuration.
func (c *Client) GetWifiWPS(ctx context.Context) (*WifiWPS, error) {
	var wps WifiWPS
	if err := c.request(ctx, http.MethodGet, "/wifi/wps", nil, &wps); err != nil {
		return nil, err
	}
	return &wps, nil
}

// UpdateWifiWPS enables or disables the device-wide WPS.
//
// Only the active flag is writable. Verified on VER-01.06.05-EA: a PUT carrying
// only "active" is accepted and leaves the device's PIN untouched, so the PIN
// (and the runtime pairing status) is never part of a write.
func (c *Client) UpdateWifiWPS(ctx context.Context, active bool) error {
	return c.request(ctx, http.MethodPut, "/wifi/wps", map[string]any{"active": active}, nil)
}

// WifiMeshMode mirrors GET /api/v1/wifi/meshmode. Mesh mode is device-wide.
type WifiMeshMode struct {
	Mode         int    `json:"mode"`
	BackhaulBand string `json:"bhBand"`

	// SupportBhApBand / SupportBhStaBand list the bands the device could use
	// for a backhaul. They are read-only and never sent back.
	SupportBhApBand  []string `json:"supportBhApBand,omitempty"`
	SupportBhStaBand []string `json:"supportBhStaBand,omitempty"`
}

// GetWifiMeshMode returns the device-wide mesh configuration.
func (c *Client) GetWifiMeshMode(ctx context.Context) (*WifiMeshMode, error) {
	var mesh WifiMeshMode
	if err := c.request(ctx, http.MethodGet, "/wifi/meshmode", nil, &mesh); err != nil {
		return nil, err
	}
	return &mesh, nil
}

// UpdateWifiMeshMode writes the device-wide mesh configuration.
//
// Only mode and bhBand are writable. Verified on VER-01.06.05-EA: a PUT
// carrying only those two is accepted (200) and the reported configuration is
// unchanged; the device's supporting-band lists are read-only and are never
// sent back.
func (c *Client) UpdateWifiMeshMode(ctx context.Context, mode int, backhaulBand string) error {
	return c.request(ctx, http.MethodPut, "/wifi/meshmode", map[string]any{"mode": mode, "bhBand": backhaulBand}, nil)
}

func isNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}
