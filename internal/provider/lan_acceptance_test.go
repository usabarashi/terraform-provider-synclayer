package provider

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestLANReadOnlyFields(t *testing.T) {
	lan := resourceLAN()
	for _, name := range []string{"ip_address", "subnet", "mac_address"} {
		field := lan.Schema[name]
		if !field.Computed || field.Optional || field.Required {
			t.Errorf("%s must be read-only (computed, not optional or required)", name)
		}
	}
	// The DHCP fields are the manageable part.
	for _, name := range []string{"dhcp_active", "dhcp_start_ip", "dhcp_end_ip", "dhcp_lease_time", "dhcp_wins_server", "dhcp_assignment"} {
		field := lan.Schema[name]
		if !field.Computed || !field.Optional {
			t.Errorf("%s should be optional and computed", name)
		}
	}
	if validate := lan.Schema["dhcp_lease_time"].ValidateFunc; validate == nil {
		t.Error("dhcp_lease_time has no ValidateFunc")
	} else if _, errs := validate(0, "dhcp_lease_time"); len(errs) == 0 {
		t.Error("a lease time of 0 should be refused")
	}
}

func TestLANLifecycle(t *testing.T) {
	f := newAdvancedFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			// A LAN has no neutral value, so destroy must leave it as the last
			// apply left it and write nothing.
			if f.lan.IPv4.IPAddress != "192.168.0.1" || f.lan.IPv4.Subnet != "255.255.255.0" {
				return fmt.Errorf("destroy changed the LAN address: %+v", f.lan.IPv4)
			}
			if f.lan.IPv4.DHCP.LeaseTime != 43200 {
				return fmt.Errorf("destroy changed the DHCP settings: %+v", f.lan.IPv4.DHCP)
			}
			if n := f.eventCount("lan_put"); n != 2 {
				return fmt.Errorf("destroy wrote the LAN settings: lan_put = %d", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Only a DHCP field is configured. The LAN address is read into
				// state and must not change.
				Config: `
resource "synclayer_sxep200w_lan" "t" {
  dhcp_lease_time = 43200
}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("synclayer_sxep200w_lan.t", "ip_address", "192.168.0.1"),
					resource.TestCheckResourceAttr("synclayer_sxep200w_lan.t", "subnet", "255.255.255.0"),
					resource.TestCheckResourceAttr("synclayer_sxep200w_lan.t", "mac_address", "02:00:00:00:00:01"),
					resource.TestCheckResourceAttr("synclayer_sxep200w_lan.t", "dhcp_lease_time", "43200"),
					func(_ *terraform.State) error {
						if f.lan.IPv4.IPAddress != "192.168.0.1" || f.lan.IPv4.Subnet != "255.255.255.0" {
							return fmt.Errorf("the write changed the LAN address: %+v", f.lan.IPv4)
						}
						if f.lan.IPv4.DHCP.LeaseTime != 43200 {
							return fmt.Errorf("lease time = %d, want 43200", f.lan.IPv4.DHCP.LeaseTime)
						}
						if f.lan.IPv4.DHCP.StartIP != "192.168.0.100" || !f.lan.IPv4.DHCP.Active {
							return fmt.Errorf("a field that was not configured changed: %+v", f.lan.IPv4.DHCP)
						}
						// The block the device does not expose is carried through.
						if f.lan.IPv6 == nil || f.lan.IPv6["mode"] != "stateless" {
							return fmt.Errorf("the IPv6 block was dropped: %+v", f.lan.IPv6)
						}
						return nil
					},
				),
			},
			{
				Config: `
resource "synclayer_sxep200w_lan" "t" {
  dhcp_lease_time = 43200
  dhcp_active     = false
}`,
				Check: func(_ *terraform.State) error {
					if f.lan.IPv4.DHCP.Active {
						return fmt.Errorf("the DHCP server was not switched off")
					}
					if f.lan.IPv4.IPAddress != "192.168.0.1" {
						return fmt.Errorf("the LAN address changed: %s", f.lan.IPv4.IPAddress)
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_lan.t",
				ImportState:       true,
				ImportStateId:     lanID,
				ImportStateVerify: true,
			},
		},
	})
}
