package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// wifiGlobalID is the fixed id of this singleton resource.
const wifiGlobalID = "global"

func resourceWifiGlobal() *schema.Resource {
	return &schema.Resource{
		Description: "Manages device-wide WiFi settings: Wi-Fi Protected Setup (WPS) " +
			"and mesh mode.\n\n" +
			"Both apply to the whole radio system rather than to one band or SSID, and " +
			"each is a single value on the device, so they share one resource. " +
			"Destroying this resource disables WPS and turns mesh mode off. Import " +
			"with the id `global`.",

		CreateContext: resourceWifiGlobalUpsert,
		ReadContext:   resourceWifiGlobalRead,
		UpdateContext: resourceWifiGlobalUpsert,
		DeleteContext: resourceWifiGlobalDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"wps_active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether Wi-Fi Protected Setup (WPS) is enabled.",
			},
			"wps_pin": {
				Type:     schema.TypeString,
				Computed: true,
				// The device generates the PIN and keeps it when a write omits
				// it (verified on VER-01.06.05-EA), so it is never configurable.
				Sensitive:   true,
				Description: "WPS PIN held by the device.",
			},
			"mesh_mode": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				// The values the web UI offers, plus "off".
				ValidateFunc: validation.IntInSlice([]int{0, 2, 3}),
				Description:  "Mesh mode: `0` off, `2` mesh agent, `3` mesh controller.",
			},
			"mesh_backhaul_band": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Band used for the mesh backhaul (the device's `bhBand`).",
			},
		},
	}
}

func resourceWifiGlobalUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	raw := d.GetRawConfig()

	if set, _ := attrConfigured(raw, "wps_active"); set {
		// Only the flag is writable, so the device's PIN is never part of the
		// request.
		if err := client.UpdateWifiWPS(ctx, d.Get("wps_active").(bool)); err != nil {
			return diag.FromErr(err)
		}
	}

	modeSet, _ := attrConfigured(raw, "mesh_mode")
	bandSet, _ := attrConfigured(raw, "mesh_backhaul_band")
	if modeSet || bandSet {
		// The write replaces both writable fields, so whichever is not
		// configured keeps its current value.
		mesh, err := client.GetWifiMeshMode(ctx)
		if err != nil {
			return diag.FromErr(err)
		}
		if modeSet {
			mesh.Mode = d.Get("mesh_mode").(int)
		}
		if bandSet {
			mesh.BackhaulBand = d.Get("mesh_backhaul_band").(string)
		}
		if err := client.UpdateWifiMeshMode(ctx, mesh.Mode, mesh.BackhaulBand); err != nil {
			return diag.FromErr(err)
		}
	}

	d.SetId(wifiGlobalID)
	return resourceWifiGlobalRead(ctx, d, meta)
}

func resourceWifiGlobalRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	wps, err := client.GetWifiWPS(ctx)
	if err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("wps_active", wps.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("wps_pin", wps.PIN); err != nil {
		return diag.FromErr(err)
	}

	mesh, err := client.GetWifiMeshMode(ctx)
	if err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("mesh_mode", mesh.Mode); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("mesh_backhaul_band", mesh.BackhaulBand); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceWifiGlobalDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	// Neither setting can be removed, so destroy turns both off.
	wps, err := client.GetWifiWPS(ctx)
	if err != nil {
		return diag.FromErr(err)
	}
	if wps.Active {
		if err := client.UpdateWifiWPS(ctx, false); err != nil {
			return diag.FromErr(err)
		}
	}

	mesh, err := client.GetWifiMeshMode(ctx)
	if err != nil {
		return diag.FromErr(err)
	}
	if mesh.Mode != 0 {
		if err := client.UpdateWifiMeshMode(ctx, 0, mesh.BackhaulBand); err != nil {
			return diag.FromErr(err)
		}
	}

	d.SetId("")
	return nil
}
