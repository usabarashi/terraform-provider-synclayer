package synclayer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// apiPrefix is prepended to every request path.
const apiPrefix = "/api/v1"

// tokenRefreshMargin is subtracted from the advertised token lifetime so that
// the token is renewed slightly before it actually expires.
const tokenRefreshMargin = 60 * time.Second

// Config holds the provider configuration needed to talk to a device.
type Config struct {
	BaseURL  string
	Username string
	Password string
	Insecure bool
	Timeout  time.Duration
}

// Client is a small REST client for the SyncLayer/SXEP200W management API.
// It transparently authenticates and refreshes the access token.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
}

// NewClient validates the configuration and returns a Client. It does not
// perform any network I/O; call Login (or any other method) to authenticate.
func NewClient(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New(`host must not be empty (set the provider "host" argument or the SYNCLAYER_HOST environment variable)`)
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	if cfg.Username == "" {
		return nil, errors.New("username must not be empty")
	}
	if cfg.Password == "" {
		return nil, errors.New("password must not be empty")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("invalid host %q: %w", cfg.BaseURL, err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid host %q: expected a URL such as http://192.168.0.1", cfg.BaseURL)
	}
	baseOrigin := baseURL.Scheme + "://" + baseURL.Host

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.Insecure} //nolint:gosec // opt-in for self-signed devices

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// The Access-Token header is a custom header, so Go would happily
		// forward it to a redirect target on another origin. Only follow
		// redirects that stay on the device.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if origin := req.URL.Scheme + "://" + req.URL.Host; origin != baseOrigin {
				return fmt.Errorf("refusing to follow redirect to %s: the device session token must not leave %s", origin, baseOrigin)
			}
			return nil
		},
	}

	return &Client{
		baseURL:  base,
		username: cfg.Username,
		password: cfg.Password,
		http:     httpClient,
	}, nil
}

// BaseURL returns the normalised base URL of the device.
func (c *Client) BaseURL() string { return c.baseURL }

// Login forces an authentication round-trip and stores the resulting token.
func (c *Client) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginLocked(ctx)
}

// Logout invalidates the session on the device. Errors are returned for
// informational purposes; callers may safely ignore them.
func (c *Client) Logout(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token == "" {
		return nil
	}
	err := c.rawRequest(ctx, http.MethodPost, "/gateway/users/logout", c.token, nil, nil)
	c.token = ""
	c.expiry = time.Time{}
	return err
}

func (c *Client) loginLocked(ctx context.Context) error {
	var auth struct {
		WebKey string `json:"web_key"`
	}
	if err := c.rawRequest(ctx, http.MethodGet, "/gateway/users/login/auth", "", nil, &auth); err != nil {
		return fmt.Errorf("fetching login challenge: %w", err)
	}
	if auth.WebKey == "" {
		return errors.New("device did not return a login challenge (web_key)")
	}

	body := map[string]string{
		"userName": c.username,
		"password": EncodeLoginPassword(c.username, c.password, auth.WebKey),
	}

	var resp struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"`
	}
	if err := c.rawRequest(ctx, http.MethodPost, "/gateway/users/login", "", body, &resp); err != nil {
		return fmt.Errorf("authenticating: %w", err)
	}
	if resp.AccessToken == "" {
		return errors.New("device did not return an access token")
	}

	ttl := time.Duration(resp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 20 * time.Minute
	}
	c.token = resp.AccessToken
	c.expiry = time.Now().Add(ttl - tokenRefreshMargin)
	return nil
}

// tokenForRequest returns a currently valid token, authenticating if needed.
func (c *Client) tokenForRequest(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}
	if err := c.loginLocked(ctx); err != nil {
		return "", err
	}
	return c.token, nil
}

// bodyBuilder lazily produces a request body from the access token that will
// actually be used for the attempt. This lets endpoints whose payload depends
// on the token (DDNS) be rebuilt after a re-authentication.
type bodyBuilder func(token string) (interface{}, error)

// request performs an authenticated request, re-authenticating once on 401.
func (c *Client) request(ctx context.Context, method, path string, body, out interface{}) error {
	return c.requestWithBody(ctx, method, path, func(string) (interface{}, error) {
		return body, nil
	}, out)
}

// requestWithBody is like request but rebuilds the body for every attempt so
// that token-derived payloads stay consistent with the token in use.
func (c *Client) requestWithBody(ctx context.Context, method, path string, build bodyBuilder, out interface{}) error {
	token, err := c.tokenForRequest(ctx)
	if err != nil {
		return err
	}

	body, err := build(token)
	if err != nil {
		return err
	}
	err = c.rawRequest(ctx, method, path, token, body, out)
	if !isUnauthorized(err) {
		return err
	}

	// Token was rejected. Discard it only if it is still the cached token:
	// a concurrent operation may already have refreshed the session, and the
	// device drops the previous session on each login, so clearing a fresh
	// token would force another (session-invalidating) login.
	c.invalidateToken(token)

	token, err = c.tokenForRequest(ctx)
	if err != nil {
		return err
	}
	body, err = build(token)
	if err != nil {
		return err
	}
	return c.rawRequest(ctx, method, path, token, body, out)
}

// invalidateToken clears the cached token only when it still matches rejected.
// It returns true when it cleared the token (the caller should re-authenticate)
// and false when another goroutine had already replaced it.
func (c *Client) invalidateToken(rejected string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != rejected {
		return false
	}
	c.token = ""
	c.expiry = time.Time{}
	return true
}

func (c *Client) rawRequest(ctx context.Context, method, path, token string, body, out interface{}) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+apiPrefix+path, reader)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Access-Token", token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("requesting %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response from %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp.StatusCode, data)
	}

	trimmed := bytes.TrimSpace(data)
	if out != nil && len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("{}")) {
		if err := json.Unmarshal(trimmed, out); err != nil {
			return fmt.Errorf("decoding response from %s %s: %w", method, path, err)
		}
	}
	return nil
}

// APIError is a structured error returned by the device.
type APIError struct {
	StatusCode int
	Code       int
	Type       string
	Message    string
}

func (e *APIError) Error() string {
	switch {
	case e.Code != 0 || e.Type != "":
		return fmt.Sprintf("device returned HTTP %d (%d %s): %s", e.StatusCode, e.Code, e.Type, e.Message)
	case e.Message != "":
		return fmt.Sprintf("device returned HTTP %d: %s", e.StatusCode, e.Message)
	default:
		return fmt.Sprintf("device returned HTTP %d", e.StatusCode)
	}
}

func parseAPIError(status int, data []byte) error {
	var envelope struct {
		Error struct {
			Code    int    `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && (envelope.Error.Message != "" || envelope.Error.Code != 0) {
		return &APIError{
			StatusCode: status,
			Code:       envelope.Error.Code,
			Type:       envelope.Error.Type,
			Message:    envelope.Error.Message,
		}
	}
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &APIError{StatusCode: status, Message: msg}
}

func isUnauthorized(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusUnauthorized || apiErr.Code == 2003
	}
	return false
}

// ---------------------------------------------------------------------------
// High level endpoints
// ---------------------------------------------------------------------------

// About returns static information about the device.
func (c *Client) About(ctx context.Context) (*About, error) {
	var about About
	if err := c.request(ctx, http.MethodGet, "/gateway/about", nil, &about); err != nil {
		return nil, err
	}
	return &about, nil
}

// --- Port forwarding -------------------------------------------------------

// ListPortForwarding returns every configured port forwarding rule.
func (c *Client) ListPortForwarding(ctx context.Context) ([]PortForwardingRule, error) {
	var resp portForwardingList
	if err := c.request(ctx, http.MethodGet, "/service/portForwarding", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

// GetPortForwarding returns a single rule by id.
func (c *Client) GetPortForwarding(ctx context.Context, id int) (*PortForwardingRule, error) {
	rules, err := c.ListPortForwarding(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i], nil
		}
	}
	return nil, nil
}

// CreatePortForwarding adds a rule and returns its device-assigned id.
func (c *Client) CreatePortForwarding(ctx context.Context, rule PortForwardingRule) (int, error) {
	var resp struct {
		ID int `json:"id"`
	}
	if err := c.request(ctx, http.MethodPost, "/service/portForwarding", rule, &resp); err != nil {
		return 0, err
	}
	if resp.ID != 0 {
		return resp.ID, nil
	}
	// Some firmware revisions answer with an empty body; fall back to a lookup.
	// The full rule identity (including protocol) is required because rules may
	// otherwise share label, address and port ranges (e.g. TCP and UDP).
	rules, err := c.ListPortForwarding(ctx)
	if err != nil {
		return 0, err
	}
	matches := make([]int, 0, 1)
	for i := range rules {
		if rules[i].ServiceType == rule.ServiceType &&
			rules[i].IPAddress == rule.IPAddress &&
			rules[i].Protocol == rule.Protocol &&
			rules[i].ExternalPort == rule.ExternalPort &&
			rules[i].LocalPort == rule.LocalPort {
			matches = append(matches, rules[i].ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return 0, errors.New("rule was created but could not be uniquely identified afterwards")
}

// UpdatePortForwarding replaces an existing rule.
func (c *Client) UpdatePortForwarding(ctx context.Context, id int, rule PortForwardingRule) error {
	rule.ID = id
	return c.request(ctx, http.MethodPut, "/service/portForwarding/"+strconv.Itoa(id), rule, nil)
}

// DeletePortForwarding removes a rule. The firmware exposes deletion as a POST.
func (c *Client) DeletePortForwarding(ctx context.Context, id int) error {
	return c.request(ctx, http.MethodPost, "/service/portForwarding/"+strconv.Itoa(id), map[string]any{}, nil)
}

// --- Static routes ---------------------------------------------------------

// ListStaticRoutes returns every configured IPv4 static route.
func (c *Client) ListStaticRoutes(ctx context.Context) ([]StaticRoute, error) {
	var resp staticRouteList
	if err := c.request(ctx, http.MethodGet, "/service/staticRoute", nil, &resp); err != nil {
		return nil, err
	}
	return resp.List, nil
}

// GetStaticRoute returns a single route by id.
func (c *Client) GetStaticRoute(ctx context.Context, id int) (*StaticRoute, error) {
	routes, err := c.ListStaticRoutes(ctx)
	if err != nil {
		return nil, err
	}
	for i := range routes {
		if routes[i].ID == id {
			return &routes[i], nil
		}
	}
	return nil, nil
}

// CreateStaticRoute adds a route and returns its device-assigned id.
func (c *Client) CreateStaticRoute(ctx context.Context, route StaticRoute) (int, error) {
	var resp struct {
		ID int `json:"id"`
	}
	if err := c.request(ctx, http.MethodPost, "/service/staticRoute", route, &resp); err != nil {
		return 0, err
	}
	if resp.ID != 0 {
		return resp.ID, nil
	}
	return c.findStaticRouteID(ctx, route)
}

// UpdateStaticRoute replaces an existing route and returns the route's id
// afterwards. The firmware implements an update as a delete followed by a
// create, so the id is not stable and must be resolved again.
func (c *Client) UpdateStaticRoute(ctx context.Context, id int, route StaticRoute) (int, error) {
	route.ID = id
	if err := c.request(ctx, http.MethodPut, "/service/staticRoute/"+strconv.Itoa(id), route, nil); err != nil {
		return 0, err
	}
	return c.findStaticRouteID(ctx, route)
}

// findStaticRouteID locates a route by its full identity (destination, subnet,
// gateway, egress interface). The interface name is only compared for WAN
// routes because the firmware reports "br0" for LAN routes.
func (c *Client) findStaticRouteID(ctx context.Context, route StaticRoute) (int, error) {
	routes, err := c.ListStaticRoutes(ctx)
	if err != nil {
		return 0, err
	}
	matches := make([]int, 0, 1)
	for i := range routes {
		if routes[i].DestinationIP != route.DestinationIP ||
			routes[i].Gateway != route.Gateway ||
			routes[i].Subnet != route.Subnet ||
			routes[i].Interface != route.Interface {
			continue
		}
		if route.Interface != 0 && routes[i].IfName != route.IfName {
			continue
		}
		matches = append(matches, routes[i].ID)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return 0, errors.New("route could not be uniquely identified after the operation")
}

// DeleteStaticRoute removes a route.
func (c *Client) DeleteStaticRoute(ctx context.Context, id int) error {
	return c.request(ctx, http.MethodDelete, "/service/staticRoute/"+strconv.Itoa(id), nil, nil)
}

// --- Reserved IPs ----------------------------------------------------------

// ListReservedIPs returns every DHCP reservation.
func (c *Client) ListReservedIPs(ctx context.Context) ([]ReservedIP, error) {
	var resp reservedIPList
	if err := c.request(ctx, http.MethodGet, "/service/reservedIP", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

// GetReservedIP returns a single reservation by id.
func (c *Client) GetReservedIP(ctx context.Context, id int) (*ReservedIP, error) {
	rules, err := c.ListReservedIPs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i], nil
		}
	}
	return nil, nil
}

// CreateReservedIP adds a reservation and returns its device-assigned id.
func (c *Client) CreateReservedIP(ctx context.Context, rule ReservedIP) (int, error) {
	body := map[string]any{"rule": rule}
	if err := c.request(ctx, http.MethodPost, "/service/reservedIP", body, nil); err != nil {
		return 0, err
	}
	rules, err := c.ListReservedIPs(ctx)
	if err != nil {
		return 0, err
	}
	for i := range rules {
		if strings.EqualFold(rules[i].MacAddress, rule.MacAddress) && rules[i].IPAddress == rule.IPAddress {
			return rules[i].ID, nil
		}
	}
	for i := range rules {
		if strings.EqualFold(rules[i].MacAddress, rule.MacAddress) {
			return rules[i].ID, nil
		}
	}
	return 0, errors.New("reservation was created but could not be found afterwards")
}

// UpdateReservedIP replaces an existing reservation. Note that the firmware
// expects the rule body unwrapped (unlike create).
func (c *Client) UpdateReservedIP(ctx context.Context, id int, rule ReservedIP) error {
	rule.ID = id
	return c.request(ctx, http.MethodPut, "/service/reservedIP/"+strconv.Itoa(id), rule, nil)
}

// DeleteReservedIP removes a reservation.
func (c *Client) DeleteReservedIP(ctx context.Context, id int) error {
	return c.request(ctx, http.MethodDelete, "/service/reservedIP/"+strconv.Itoa(id), nil, nil)
}

// --- DDNS ------------------------------------------------------------------

// GetDDNS returns the current DDNS configuration and status.
func (c *Client) GetDDNS(ctx context.Context) (*DDNS, error) {
	var ddns DDNS
	if err := c.request(ctx, http.MethodGet, "/service/ddns", nil, &ddns); err != nil {
		return nil, err
	}
	return &ddns, nil
}

// UpdateDDNS applies a DDNS configuration. The password is encrypted with the
// scheme expected by the firmware (see EncodeDDNSPassword). The full document
// is sent because the device validates the accompanying provider list.
func (c *Client) UpdateDDNS(ctx context.Context, active bool, cfg DDNSConfiguration) (*DDNSResult, error) {
	current, err := c.GetDDNS(ctx)
	if err != nil {
		return nil, err
	}

	// The password is encrypted with the token in use, so it must be rebuilt
	// for each attempt (including after a transparent re-authentication).
	plainPassword := cfg.Password
	cfg.Password = ""

	build := func(token string) (interface{}, error) {
		encoded, err := EncodeDDNSPassword(token, cfg.Username, plainPassword)
		if err != nil {
			return nil, err
		}
		effective := cfg
		effective.Password = encoded
		return map[string]any{
			"active":             active,
			"debugKey":           "",
			"result":             current.Result,
			"supportingProvider": current.SupportingProvider,
			"configuration":      effective,
		}, nil
	}

	var result DDNSResult
	if err := c.requestWithBody(ctx, http.MethodPut, "/service/ddns", build, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// --- DMZ -------------------------------------------------------------------

// GetDMZ returns the current DMZ configuration.
func (c *Client) GetDMZ(ctx context.Context) (*DMZ, error) {
	var dmz DMZ
	if err := c.request(ctx, http.MethodGet, "/service/dmz", nil, &dmz); err != nil {
		return nil, err
	}
	return &dmz, nil
}

// UpdateDMZ applies the DMZ configuration.
func (c *Client) UpdateDMZ(ctx context.Context, dmz DMZ) error {
	return c.request(ctx, http.MethodPut, "/service/dmz", dmz, nil)
}
