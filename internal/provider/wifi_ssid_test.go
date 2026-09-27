package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestParseWifiSSIDID(t *testing.T) {
	cases := []struct {
		id     string
		band   int
		index  int
		errSub string
	}{
		{"2.4g/0", 0, 0, ""},
		{"2.4g/1", 0, 1, ""},
		{"5g/1", 1, 1, ""},
		// Extra slots stay parseable so a resource managed before the
		// restriction can still be read and deleted. The restriction is applied
		// by the schema validation and by importWifiSSID.
		{"5g/2", 1, 2, ""},
		{"2.4g/-1", 0, 0, "must be >= 0"},
		{"2.4g/x", 0, 0, "invalid SSID index"},
		{"5ghz/0", 0, 0, "unsupported band"},
		{"5g", 0, 0, "expected id in the form"},
	}

	for _, c := range cases {
		band, index, err := parseWifiSSIDID(c.id)
		if c.errSub != "" {
			if err == nil || !strings.Contains(err.Error(), c.errSub) {
				t.Errorf("%s: got error %v, want one containing %q", c.id, err, c.errSub)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.id, err)
			continue
		}
		if band != c.band || index != c.index {
			t.Errorf("%s: got band=%d index=%d, want band=%d index=%d", c.id, band, index, c.band, c.index)
		}
	}
}

func TestImportWifiSSIDRejectsExtraSlots(t *testing.T) {
	r := resourceWifiSSID()
	importID := func(t *testing.T, id string) error {
		t.Helper()
		d := schema.TestResourceDataRaw(t, r.Schema, nil)
		d.SetId(id)
		_, err := importWifiSSID(context.Background(), d, nil)
		return err
	}

	if err := importID(t, "5g/0"); err != nil {
		t.Errorf("5g/0 should import: %v", err)
	}
	if err := importID(t, "2.4g/1"); err != nil {
		t.Errorf("2.4g/1 should import: %v", err)
	}
	if err := importID(t, "5g/2"); err == nil || !strings.Contains(err.Error(), "is not manageable") {
		t.Errorf("5g/2 should be refused, got %v", err)
	}
	// Malformed ids are refused too.
	for _, id := range []string{"5g", "2.4g/-1", "5ghz/0", "5g/x"} {
		if err := importID(t, id); err != nil {
			continue
		}
		t.Errorf("%s should be refused", id)
	}
}

func TestWifiSSIDIndexSchemaValidation(t *testing.T) {
	validate := resourceWifiSSID().Schema["index"].ValidateFunc
	if validate == nil {
		t.Fatal("index has no ValidateFunc")
	}

	for _, index := range []int{0, 1} {
		if _, errs := validate(index, "index"); len(errs) > 0 {
			t.Errorf("index %d should be accepted: %v", index, errs)
		}
	}
	if _, errs := validate(2, "index"); len(errs) == 0 {
		t.Error("index 2 should be refused by the schema")
	}
}

func TestWifiSSIDAccessControlRulesSchema(t *testing.T) {
	rules := resourceWifiSSID().Schema["access_control_rules"]

	// Addresses are hashed case-insensitively, so a lower-case configuration
	// matches the upper-case form the device reports.
	set := rules.Set
	if set == nil {
		t.Fatal("access_control_rules has no Set function")
	}
	if set("aa:bb:cc:dd:ee:01") != set("AA:BB:CC:DD:EE:01") {
		t.Error("MAC addresses should hash case-insensitively")
	}

	validate := rules.Elem.(*schema.Schema).ValidateFunc
	if validate == nil {
		t.Fatal("access_control_rules has no ValidateFunc")
	}
	for _, mac := range []string{"aa:bb:cc:dd:ee:01", "AA:BB:CC:DD:EE:01"} {
		if _, errs := validate(mac, "access_control_rules"); len(errs) > 0 {
			t.Errorf("%s should be accepted: %v", mac, errs)
		}
	}
	for _, mac := range []string{"nonsense", "aa:bb:cc:dd:ee", "aa:bb:cc:dd:ee:01:02"} {
		if _, errs := validate(mac, "access_control_rules"); len(errs) == 0 {
			t.Errorf("%s should be refused by the schema", mac)
		}
	}
}
