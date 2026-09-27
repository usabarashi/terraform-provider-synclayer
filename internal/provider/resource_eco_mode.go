package provider

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

// ecoModeID is the fixed id of this singleton resource.
const ecoModeID = "eco_mode"

var timeOfDayRegexp = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func resourceEcoMode() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the device's eco mode.\n\n" +
			"Eco mode can be switched on for its own schedule. Destroying this " +
			"resource turns eco mode off and disables its schedule, so a schedule " +
			"cannot be left behind to run the next time it is switched on. Import " +
			"with the id `eco_mode`.",

		CreateContext: resourceEcoModeUpsert,
		ReadContext:   resourceEcoModeRead,
		UpdateContext: resourceEcoModeUpsert,
		DeleteContext: resourceEcoModeDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether eco mode is enabled.",
			},
			"type": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				// The device reports a number here and its web UI offers a set of
				// modes; the values are passed through as reported rather than
				// guessed at.
				Description: "Eco mode type, as the device reports it.",
			},
			"schedule_enabled": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether eco mode follows its own schedule.",
			},
			"start_time": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ValidateFunc: validation.StringMatch(timeOfDayRegexp, "must be a time of day such as 21:00"),
				Description:  "Start of the schedule, as `HH:MM`.",
			},
			"end_time": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ValidateFunc: validation.StringMatch(timeOfDayRegexp, "must be a time of day such as 06:00"),
				Description:  "End of the schedule, as `HH:MM`.",
			},
		},
	}
}

func resourceEcoModeUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	// The device replaces the whole object, so it is read first and only the
	// configured fields are changed in it.
	current, err := client.GetEcoMode(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	raw := d.GetRawConfig()
	if set, _ := attrConfigured(raw, "active"); set {
		current.Active = d.Get("active").(bool)
	}
	if set, _ := attrConfigured(raw, "type"); set {
		current.Type = d.Get("type").(int)
	}
	if set, _ := attrConfigured(raw, "schedule_enabled"); set {
		current.ScheduleEnabled = d.Get("schedule_enabled").(bool)
	}
	if set, _ := attrConfigured(raw, "start_time"); set {
		current.StartTime = d.Get("start_time").(string)
	}
	if set, _ := attrConfigured(raw, "end_time"); set {
		current.EndTime = d.Get("end_time").(string)
	}

	d.SetId(ecoModeID)

	if err := client.UpdateEcoMode(ctx, *current); err != nil {
		return diag.FromErr(err)
	}
	if err := waitForEcoMode(ctx, client, *current); err != nil {
		return diag.FromErr(err)
	}

	return resourceEcoModeRead(ctx, d, meta)
}

// waitForEcoMode gives an accepted write a few chances to become visible: the
// device answers 202 before it has applied the change, so a read straight
// afterwards can still show the old settings, and recording those would leave
// the state describing something the device never applied. The wait is bounded,
// so a value the device adjusts rather than stores cannot hang an apply.
func waitForEcoMode(ctx context.Context, client *synclayer.Client, want synclayer.EcoMode) error {
	for attempt := 0; attempt < 5; attempt++ {
		read, err := client.GetEcoMode(ctx)
		if err != nil {
			return err
		}
		if *read == want || attempt == 4 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil
}

func resourceEcoModeRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetEcoMode(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("active", current.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("type", current.Type); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("schedule_enabled", current.ScheduleEnabled); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("start_time", current.StartTime); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("end_time", current.EndTime); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceEcoModeDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	// Eco mode has an off state, and its schedule goes with it: a schedule left
	// behind would run the next time eco mode is switched on, by Terraform or
	// by hand.
	current, err := client.GetEcoMode(ctx)
	if err != nil {
		return diag.FromErr(err)
	}
	if current.Active || current.ScheduleEnabled {
		current.Active = false
		current.ScheduleEnabled = false
		if err := client.UpdateEcoMode(ctx, *current); err != nil {
			return diag.FromErr(err)
		}
		if err := waitForEcoMode(ctx, client, *current); err != nil {
			return diag.FromErr(err)
		}
	}

	d.SetId("")
	return nil
}
