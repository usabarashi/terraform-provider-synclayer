package synclayer

import (
	"context"
	"net/http"
)

// LANDHCP is the DHCP server block of the LAN.
type LANDHCP struct {
	Active     bool   `json:"active"`
	StartIP    string `json:"startIP"`
	EndIP      string `json:"endIP"`
	LeaseTime  int    `json:"leaseTime"`
	WinsServer string `json:"winsServer"`
	Assignment string `json:"assignment"`
}

// LANIPv4 is the IPv4 half of the LAN settings.
type LANIPv4 struct {
	IPAddress string  `json:"ipAddress"`
	Subnet    string  `json:"subnet"`
	DHCP      LANDHCP `json:"dhcp"`
}

// LAN mirrors GET /api/v1/network/lan.
//
// The IPv6 block is carried through as it was read so that a
// read-modify-write round trip does not drop it. The device does not expose it
// for configuration here (its own endpoint answers 501 on this firmware).
type LAN struct {
	MACAddress string         `json:"macAddress,omitempty"`
	IPv4       LANIPv4        `json:"ipv4"`
	IPv6       map[string]any `json:"ipv6,omitempty"`
}

// GetLAN returns the LAN settings.
func (c *Client) GetLAN(ctx context.Context) (*LAN, error) {
	var lan LAN
	if err := c.request(ctx, http.MethodGet, "/network/lan", nil, &lan); err != nil {
		return nil, err
	}
	return &lan, nil
}

// UpdateLAN writes the LAN settings.
//
// The whole object is replaced, so callers must replay the values they read for
// everything they do not mean to change — including the LAN address itself
// (verified on VER-01.06.05-EA: replaying the object leaves the address, the
// subnet and the DHCP range untouched).
func (c *Client) UpdateLAN(ctx context.Context, lan LAN) error {
	return c.request(ctx, http.MethodPut, "/network/lan", lan, nil)
}
