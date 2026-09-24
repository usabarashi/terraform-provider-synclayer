package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

func TestProviderInternalValidate(t *testing.T) {
	if err := New().InternalValidate(); err != nil {
		t.Fatalf("provider schema is invalid: %v", err)
	}
}

func TestReservedIPMacDiffSuppression(t *testing.T) {
	suppress := resourceReservedIP().Schema["mac_address"].DiffSuppressFunc
	if suppress == nil {
		t.Fatal("mac_address has no DiffSuppressFunc")
	}

	if !suppress("mac_address", "AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff", nil) {
		t.Fatal("a case-only MAC change should be suppressed")
	}
	if suppress("mac_address", "AA:BB:CC:DD:EE:FF", "AA:BB:CC:DD:EE:00", nil) {
		t.Fatal("a different MAC address must not be suppressed")
	}
}

// TestStaticRouteInterfaceNameNormalisation guards against a stale WAN
// interface name leaking into a LAN route.
func TestStaticRouteInterfaceNameNormalisation(t *testing.T) {
	r := resourceStaticRoute()

	d := schema.TestResourceDataRaw(t, r.Schema, map[string]interface{}{
		"destination_ip": "10.0.0.0",
		"subnet":         "255.0.0.0",
		"gateway":        "192.168.0.1",
		"interface":      "wan",
		"interface_name": "veip0.1",
	})

	wan := &synclayer.StaticRoute{
		ID: 1, Active: true, DestinationIP: "10.0.0.0", Subnet: "255.0.0.0",
		Gateway: "192.168.0.1", Interface: 1, IfName: "veip0.1",
	}
	if err := flattenStaticRoute(d, wan); err != nil {
		t.Fatalf("flatten WAN route: %v", err)
	}
	if got := d.Get("interface_name").(string); got != "veip0.1" {
		t.Fatalf("WAN route interface_name = %q, want %q", got, "veip0.1")
	}

	lan := &synclayer.StaticRoute{
		ID: 1, Active: true, DestinationIP: "10.0.0.0", Subnet: "255.0.0.0",
		Gateway: "192.168.0.1", Interface: 0, IfName: "br0",
	}
	if err := flattenStaticRoute(d, lan); err != nil {
		t.Fatalf("flatten LAN route: %v", err)
	}
	if got := d.Get("interface_name").(string); got != "" {
		t.Fatalf("LAN route must clear interface_name, got %q", got)
	}

	// Expanding a LAN route must never transmit a WAN interface name.
	config := schema.TestResourceDataRaw(t, r.Schema, map[string]interface{}{
		"destination_ip": "10.0.0.0",
		"subnet":         "255.0.0.0",
		"gateway":        "192.168.0.1",
		"interface":      "lan",
		"interface_name": "veip0.1",
	})
	route := expandStaticRoute(config)
	if route.Interface != 0 || route.IfName != "" {
		t.Fatalf("LAN route must not carry a WAN name: %+v", route)
	}
}
