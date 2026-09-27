package provider

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestEcoModeLifecycle(t *testing.T) {
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
			// Eco mode has an off state, and the schedule goes with it.
			if f.eco.Active {
				return fmt.Errorf("eco mode is still on after destroy: %+v", f.eco)
			}
			if f.eco.ScheduleEnabled {
				return fmt.Errorf("the schedule is still on after destroy: %+v", f.eco)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Only the schedule is configured; everything else keeps its
				// device value.
				Config: `
resource "synclayer_sxep200w_eco_mode" "t" {
  start_time = "23:00"
  end_time   = "07:00"
}`,
				Check: func(_ *terraform.State) error {
					if f.eco.StartTime != "23:00" || f.eco.EndTime != "07:00" {
						return fmt.Errorf("schedule = %q-%q, want 23:00-07:00", f.eco.StartTime, f.eco.EndTime)
					}
					if f.eco.Active || f.eco.Type != 1 {
						return fmt.Errorf("an unconfigured field changed: %+v", f.eco)
					}
					return nil
				},
			},
			{
				Config: `
resource "synclayer_sxep200w_eco_mode" "t" {
  active           = true
  schedule_enabled = true
  start_time       = "22:30"
  end_time         = "06:30"
}`,
				Check: func(_ *terraform.State) error {
					if !f.eco.Active || !f.eco.ScheduleEnabled {
						return fmt.Errorf("eco mode was not switched on: %+v", f.eco)
					}
					if f.eco.StartTime != "22:30" || f.eco.EndTime != "06:30" {
						return fmt.Errorf("schedule = %q-%q", f.eco.StartTime, f.eco.EndTime)
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_eco_mode.t",
				ImportState:       true,
				ImportStateId:     ecoModeID,
				ImportStateVerify: true,
			},
		},
	})
}

func TestDateTimeLifecycle(t *testing.T) {
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
			// There is no neutral time zone or clock to write back, so destroy
			// must not touch the device: the two steps above are the only writes.
			if n := f.eventCount("datetime_put"); n != 2 {
				return fmt.Errorf("destroy wrote the settings: datetime_put = %d", n)
			}
			if f.date.TimeZone != 127 || !f.date.NTP.Active {
				return fmt.Errorf("the settings changed on destroy: %+v", f.date)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Only NTP is configured: the time zone and daylight saving keep
				// their device values.
				Config: `
resource "synclayer_sxep200w_datetime" "t" {
  ntp_active = true
  ntp_servers = ["ntp.example.test"]
}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("synclayer_sxep200w_datetime.t", "time_zone", "127"),
					resource.TestCheckResourceAttr("synclayer_sxep200w_datetime.t", "time_zone_name", "Tokyo"),
					func(_ *terraform.State) error {
						if len(f.date.NTP.Servers) != 1 || f.date.NTP.Servers[0] != "ntp.example.test" {
							return fmt.Errorf("servers = %+v", f.date.NTP.Servers)
						}
						if f.date.DaylightSaving.Active {
							return fmt.Errorf("an unconfigured field changed: %+v", f.date)
						}
						return nil
					},
				),
			},
			{
				Config: `
resource "synclayer_sxep200w_datetime" "t" {
  ntp_active              = true
  ntp_servers             = ["ntp.example.test"]
  time_zone               = 127
  daylight_saving_active  = true
  daylight_saving_start   = "03/29"
  daylight_saving_end     = "10/25"
}`,
				Check: func(_ *terraform.State) error {
					if !f.date.DaylightSaving.Active {
						return fmt.Errorf("daylight saving was not enabled: %+v", f.date.DaylightSaving)
					}
					if f.date.DaylightSaving.StartDate != "03/29" || f.date.DaylightSaving.EndDate != "10/25" {
						return fmt.Errorf("daylight saving dates = %+v", f.date.DaylightSaving)
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_datetime.t",
				ImportState:       true,
				ImportStateId:     dateTimeID,
				ImportStateVerify: true,
			},
		},
	})
}
