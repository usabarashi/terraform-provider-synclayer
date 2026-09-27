package provider

import (
	"context"
	"fmt"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

// TestPacketFilteringRefusesOccupiedPriority pins the one behaviour that
// protects the device's own rules: the device would happily replace whatever
// sits at a priority, so the provider has to refuse.
func TestPacketFilteringRefusesOccupiedPriority(t *testing.T) {
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
		Steps: []resource.TestStep{
			{
				// The fake starts with a rule at priority 1.
				Config: `
resource "synclayer_sxep200w_packet_filtering" "t" {
  family      = "ipv4"
  priority    = 1
  filter_type = "deny"
  protocol    = "tcp"
}`,
				ExpectError: regexp.MustCompile("is already used"),
			},
		},
	})

	if n := f.eventCount("access_control_create"); n != 0 {
		t.Errorf("a rule was written over an occupied priority: %d create(s)", n)
	}
	if len(f.acRules["ipv4"]) != 1 || f.acRules["ipv4"][0].Dest.StartPort != 137 {
		t.Errorf("the existing rule was disturbed: %+v", f.acRules["ipv4"])
	}
}

func TestPacketFilteringLifecycle(t *testing.T) {
	f := newAdvancedFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	find := func(index int) *struct {
		FilterType string
		StartPort  int
	} {
		for _, r := range f.acRules["ipv4"] {
			if r.Index == index {
				return &struct {
					FilterType string
					StartPort  int
				}{r.FilterType, r.Dest.StartPort}
			}
		}
		return nil
	}

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			if len(f.acRules["ipv4"]) != 1 {
				return fmt.Errorf("destroy left rules behind: %+v", f.acRules["ipv4"])
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: `
resource "synclayer_sxep200w_packet_filtering" "t" {
  family      = "ipv4"
  priority    = 20
  filter_type = "deny"
  protocol    = "udp"
  description = "block the test port"

  src {
    type       = "any"
    ip_address = "0.0.0.0"
    start_port = 1
    end_port   = 1
  }

  dest {
    type       = "any"
    ip_address = "0.0.0.0"
    start_port = 59999
    end_port   = 59999
  }
}`,
				Check: func(_ *terraform.State) error {
					got := find(20)
					if got == nil {
						return fmt.Errorf("the rule was not created: %+v", f.acRules["ipv4"])
					}
					if got.FilterType != "deny" || got.StartPort != 59999 {
						return fmt.Errorf("rule 20 = %+v", *got)
					}
					return nil
				},
			},
			{
				// An update stays at its priority: the device updates in place.
				Config: `
resource "synclayer_sxep200w_packet_filtering" "t" {
  family      = "ipv4"
  priority    = 20
  filter_type = "allow"
  protocol    = "tcp"
  description = "block the test port"

  src {
    type       = "any"
    ip_address = "0.0.0.0"
    start_port = 1
    end_port   = 1
  }

  dest {
    type       = "any"
    ip_address = "0.0.0.0"
    start_port = 59998
    end_port   = 59998
  }
}`,
				Check: func(_ *terraform.State) error {
					got := find(20)
					if got == nil {
						return fmt.Errorf("the rule disappeared on update: %+v", f.acRules["ipv4"])
					}
					if got.FilterType != "allow" || got.StartPort != 59998 {
						return fmt.Errorf("rule 20 after update = %+v", *got)
					}
					if len(f.acRules["ipv4"]) != 2 {
						return fmt.Errorf("the update moved the rule: %+v", f.acRules["ipv4"])
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_packet_filtering.t",
				ImportState:       true,
				ImportStateId:     "ipv4/20",
				ImportStateVerify: true,
			},
		},
	})
}

func TestPortTriggeringLifecycle(t *testing.T) {
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
			if len(f.triggers) != 0 {
				return fmt.Errorf("destroy left rules behind: %+v", f.triggers)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: `
resource "synclayer_sxep200w_port_triggering" "t" {
  description = "game"

  triggered {
    protocol    = "tcp"
    start_range = 59998
    end_range   = 59998
  }

  forwarded {
    protocol    = "tcp"
    start_range = 59997
    end_range   = 59997
  }
}`,
				Check: func(_ *terraform.State) error {
					if len(f.triggers) != 1 {
						return fmt.Errorf("rules = %d, want 1", len(f.triggers))
					}
					got := f.triggers[0]
					if got.Triggered.StartRange != 59998 || got.Forwarded.EndRange != 59997 {
						return fmt.Errorf("rule = %+v", got)
					}
					return nil
				},
			},
			{
				// A disabled rule stays in the device's list, so switching it
				// off is an update and not a delete.
				Config: `
resource "synclayer_sxep200w_port_triggering" "t" {
  active      = false
  description = "game"

  triggered {
    protocol    = "tcp"
    start_range = 59998
    end_range   = 59998
  }

  forwarded {
    protocol    = "tcp"
    start_range = 59997
    end_range   = 59997
  }
}`,
				Check: func(_ *terraform.State) error {
					if len(f.triggers) != 1 {
						return fmt.Errorf("the disabled rule was removed: %+v", f.triggers)
					}
					if f.triggers[0].Active {
						return fmt.Errorf("the rule was not disabled")
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_port_triggering.t",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestPacketFilteringDoesNotKeepARefusedPriority covers the race the two checks
// leave open: another writer takes the priority between the resource's check and
// the client's, so nothing is written. The resource must not come out of that
// claiming the priority, because a later destroy would delete the other rule.
func TestPacketFilteringDoesNotKeepARefusedPriority(t *testing.T) {
	f := newAdvancedFake()
	f.acInterleave = true
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	client, err := synclayer.NewClient(synclayer.Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}

	r := resourcePacketFiltering()
	d := schema.TestResourceDataRaw(t, r.Schema, map[string]interface{}{
		"family":   "ipv4",
		"priority": 20,
	})

	if diags := resourcePacketFilteringCreate(ctx, d, client); !diags.HasError() {
		t.Fatal("expected the create to be refused")
	}
	if d.Id() != "" {
		t.Errorf("id = %q after a refused create, want it empty: nothing was written and a destroy would delete another rule", d.Id())
	}
	if n := f.eventCount("access_control_create"); n != 0 {
		t.Errorf("the rule was written anyway: %d create(s)", n)
	}
	found := false
	for _, rule := range f.acRules["ipv4"] {
		if rule.Index == 20 && rule.Description == "someone else" {
			found = true
		}
	}
	if !found {
		t.Errorf("the other writer's rule was disturbed: %+v", f.acRules["ipv4"])
	}
}

// TestPortTriggeringCreateIgnoresOtherNewRules covers the same kind of race on
// the other endpoint: a second rule appears between the two reads, so the
// created rule has to be identified by its content rather than by being the
// only new one.
func TestPortTriggeringCreateIgnoresOtherNewRules(t *testing.T) {
	f := newAdvancedFake()
	f.triggerDecoy = true
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	client, err := synclayer.NewClient(synclayer.Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}

	rule := synclayer.PortTriggeringRule{
		Active:      true,
		Description: "mine",
		Triggered:   synclayer.PortTriggeringRange{Protocol: "tcp", StartRange: 59998, EndRange: 59998},
		Forwarded:   synclayer.PortTriggeringRange{Protocol: "tcp", StartRange: 59997, EndRange: 59997},
	}
	id, err := client.CreatePortTriggeringRule(ctx, rule)
	if err != nil {
		t.Fatalf("CreatePortTriggeringRule: %v", err)
	}
	got, err := client.GetPortTriggeringRule(ctx, id)
	if err != nil {
		t.Fatalf("GetPortTriggeringRule: %v", err)
	}
	if got == nil || got.Description != "mine" {
		t.Errorf("resolved id %d to the wrong rule: %+v", id, got)
	}
}
