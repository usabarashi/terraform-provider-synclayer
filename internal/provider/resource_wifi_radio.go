package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

func resourceWifiRadio() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the radio settings of a WiFi band.\n\n" +
			"Each band (`2.4g`, `5g`) has exactly one radio. Destroying this " +
			"resource turns the radio off. Import with the band name, e.g. `5g`.",

		CreateContext: resourceWifiRadioUpsert,
		ReadContext:   resourceWifiRadioRead,
		UpdateContext: resourceWifiRadioUpsert,
		DeleteContext: resourceWifiRadioDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"band": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringInSlice([]string{"2.4g", "5g"}, false),
				Description:  "Radio band: `2.4g` or `5g`.",
			},
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether the radio is enabled.",
			},
			"wireless_mode": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Supported 802.11 modes, e.g. `802.11b+g+n+ax`.",
			},
			"channel": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Channel number, or `auto`.",
			},
			"bandwidth": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Channel bandwidth in MHz, e.g. `20`, `40`, `80`.",
			},
			"output_power": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Transmit power level, e.g. `high`, `medium`, `low`.",
			},
		},
	}
}

func resourceWifiRadioUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	band, err := wifiBandID(d.Get("band").(string))
	if err != nil {
		return diag.FromErr(err)
	}

	current, err := client.GetWifiRadio(ctx, band)
	if err != nil {
		return diag.FromErr(err)
	}

	raw := d.GetRawConfig()
	if set, _ := attrConfigured(raw, "active"); set {
		current.Active = d.Get("active").(bool)
	}
	if set, _ := attrConfigured(raw, "wireless_mode"); set {
		current.Basic.WirelessMode = d.Get("wireless_mode").(string)
	}
	if set, _ := attrConfigured(raw, "channel"); set {
		current.Basic.Channel.Set = d.Get("channel").(string)
	}
	if set, _ := attrConfigured(raw, "bandwidth"); set {
		current.Basic.Bandwidth.Set = d.Get("bandwidth").(string)
	}
	if set, _ := attrConfigured(raw, "output_power"); set {
		current.Basic.OutputPower = d.Get("output_power").(string)
	}

	if err := client.UpdateWifiRadio(ctx, band, *current); err != nil {
		return diag.FromErr(err)
	}

	d.SetId(wifiBandName(band))
	return resourceWifiRadioRead(ctx, d, meta)
}

func resourceWifiRadioRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	band, err := wifiBandID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	radio, err := client.GetWifiRadio(ctx, band)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("band", wifiBandName(band)); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("active", radio.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("wireless_mode", radio.Basic.WirelessMode); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("channel", radio.Basic.Channel.Set); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("bandwidth", radio.Basic.Bandwidth.Set); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("output_power", radio.Basic.OutputPower); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourceWifiRadioDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	band, err := wifiBandID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	current, err := client.GetWifiRadio(ctx, band)
	if err != nil {
		return diag.FromErr(err)
	}
	if current != nil && current.Active {
		// A band's radio cannot be removed, so destroy turns it off.
		current.Active = false
		if err := client.UpdateWifiRadio(ctx, band, *current); err != nil {
			return diag.FromErr(err)
		}
	}
	d.SetId("")
	return nil
}
