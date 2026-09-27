package provider

import (
	"fmt"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestWifiGlobalMeshModeSchemaValidation(t *testing.T) {
	validate := resourceWifiGlobal().Schema["mesh_mode"].ValidateFunc
	if validate == nil {
		t.Fatal("mesh_mode has no ValidateFunc")
	}

	for _, mode := range []int{0, 2, 3} {
		if _, errs := validate(mode, "mesh_mode"); len(errs) > 0 {
			t.Errorf("mesh_mode %d should be accepted: %v", mode, errs)
		}
	}
	for _, mode := range []int{1, 4} {
		if _, errs := validate(mode, "mesh_mode"); len(errs) == 0 {
			t.Errorf("mesh_mode %d should be refused by the schema", mode)
		}
	}
}

func TestWifiGlobalLifecycle(t *testing.T) {
	f := newWifiFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	checkGlobal := func(wpsActive bool, meshMode int, meshBand string) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.wpsActive != wpsActive {
				return fmt.Errorf("device WPS active = %v, want %v", f.wpsActive, wpsActive)
			}
			if f.meshMode != meshMode {
				return fmt.Errorf("device mesh mode = %d, want %d", f.meshMode, meshMode)
			}
			if f.meshBand != meshBand {
				return fmt.Errorf("device mesh backhaul band = %q, want %q", f.meshBand, meshBand)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			// Neither setting can be removed, so destroy turns both off and
			// leaves the PIN alone.
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.wpsActive {
				return fmt.Errorf("WPS is still enabled after destroy")
			}
			if f.meshMode != 0 {
				return fmt.Errorf("mesh mode = %d after destroy, want 0", f.meshMode)
			}
			if f.wpsPIN != "00000000" {
				return fmt.Errorf("WPS PIN changed on destroy: %q", f.wpsPIN)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Only WPS is declared: mesh keeps its device value.
				Config: `
resource "synclayer_sxep200w_wifi_global" "t" {
  wps_active = false
}`,
				Check: checkGlobal(false, 0, ""),
			},
			{
				Config: `
resource "synclayer_sxep200w_wifi_global" "t" {
  wps_active         = true
  mesh_mode          = 3
  mesh_backhaul_band = "6"
}`,
				Check: checkGlobal(true, 3, "6"),
			},
			{
				ResourceName:      "synclayer_sxep200w_wifi_global.t",
				ImportState:       true,
				ImportStateId:     "global",
				ImportStateVerify: true,
			},
		},
	})
}

func TestWifiGlobalPartialFailureKeepsID(t *testing.T) {
	// A create writes two device settings. When the second write fails, the
	// first must not be left applied but untracked: the id is recorded before
	// anything is written, so the resource stays in state and its destroy can
	// still switch the applied setting back off.
	f := newWifiFake()
	f.wpsActive = false
	f.failMeshWrite = true
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
				Config: `
resource "synclayer_sxep200w_wifi_global" "t" {
  wps_active = true
  mesh_mode  = 3
}`,
				ExpectError: regexp.MustCompile("simulated failure"),
			},
		},
	})

	// The failed create left the resource in state, so the harness tore it down
	// on the way out, and that teardown is what switched WPS back off. Had the
	// id not been recorded before the first write, there would be nothing in
	// state to tear down and WPS would still be on.
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.wpsActive {
		t.Error("WPS is still enabled: the failed create kept no resource in state")
	}
}
