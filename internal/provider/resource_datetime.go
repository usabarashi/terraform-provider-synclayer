package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// dateTimeID is the fixed id of this singleton resource.
const dateTimeID = "datetime"

func resourceDateTime() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the device's date and time settings: NTP, time zone and " +
			"daylight saving.\n\n" +
			"The device's current time is not managed here — it comes from NTP, and a " +
			"clock is not a setting. Destroying this resource removes it from state and " +
			"leaves the device as it is, because there is no neutral time zone or clock " +
			"to write back. Import with the id `datetime`.",

		CreateContext: resourceDateTimeUpsert,
		ReadContext:   resourceDateTimeRead,
		UpdateContext: resourceDateTimeUpsert,
		DeleteContext: resourceDateTimeDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"ntp_active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether the device takes its time from NTP.",
			},
			"ntp_servers": {
				Type:        schema.TypeList,
				Optional:    true,
				Computed:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "NTP servers, in the order the device should use them.",
			},
			"time_zone": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				Description: "Time zone, as the index the device uses (its web UI shows the " +
					"matching name; `time_zone_name` reports it).",
			},
			"time_zone_name": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Name of the time zone the device reports for `time_zone`.",
			},
			"daylight_saving_active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether daylight saving is enabled.",
			},
			"daylight_saving_start": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Start of daylight saving, in the format the device expects.",
			},
			"daylight_saving_end": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "End of daylight saving, in the format the device expects.",
			},
		},
	}
}

func resourceDateTimeUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	// The write replaces the whole object, so it starts from what the device
	// reports and only changes what is configured.
	current, err := client.GetDateTime(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	raw := d.GetRawConfig()
	if set, _ := attrConfigured(raw, "ntp_active"); set {
		current.NTP.Active = d.Get("ntp_active").(bool)
	}
	if set, _ := attrConfigured(raw, "ntp_servers"); set {
		current.NTP.Servers = expandStringList(d.Get("ntp_servers").([]interface{}))
	}
	if set, _ := attrConfigured(raw, "time_zone"); set {
		current.TimeZone = d.Get("time_zone").(int)
	}
	if set, _ := attrConfigured(raw, "daylight_saving_active"); set {
		current.DaylightSaving.Active = d.Get("daylight_saving_active").(bool)
	}
	if set, _ := attrConfigured(raw, "daylight_saving_start"); set {
		current.DaylightSaving.StartDate = d.Get("daylight_saving_start").(string)
	}
	if set, _ := attrConfigured(raw, "daylight_saving_end"); set {
		current.DaylightSaving.EndDate = d.Get("daylight_saving_end").(string)
	}

	d.SetId(dateTimeID)

	if err := client.UpdateDateTime(ctx, *current); err != nil {
		return diag.FromErr(err)
	}

	return resourceDateTimeRead(ctx, d, meta)
}

func resourceDateTimeRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetDateTime(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("ntp_active", current.NTP.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ntp_servers", current.NTP.Servers); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("time_zone", current.TimeZone); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("time_zone_name", current.TimeZoneName); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("daylight_saving_active", current.DaylightSaving.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("daylight_saving_start", current.DaylightSaving.StartDate); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("daylight_saving_end", current.DaylightSaving.EndDate); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceDateTimeDelete(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
	// There is no neutral time zone or clock to write back, so only the
	// Terraform record goes away.
	d.SetId("")
	return nil
}

// expandStringList converts a flat list attribute into strings.
func expandStringList(raw []interface{}) []string {
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}
