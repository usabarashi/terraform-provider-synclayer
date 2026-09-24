package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

const dmzID = "dmz"

func resourceDMZ() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the DMZ host of the device.\n\n" +
			"This is a singleton resource: there is only one DMZ host per device. " +
			"Destroying the resource disables the DMZ instead of deleting it. " +
			"Import it with the id `dmz`.",

		CreateContext: resourceDmzUpsert,
		ReadContext:   resourceDmzRead,
		UpdateContext: resourceDmzUpsert,
		DeleteContext: resourceDmzDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Whether the DMZ host is enabled.",
			},
			"destination": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ValidateFunc: validation.IsIPv4Address,
				Description: "LAN IPv4 address that receives all unsolicited inbound traffic. " +
					"Meaningful when `active` is true; the device reports `0.0.0.0` while disabled.",
			},
			"subnet": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ValidateFunc: validation.IsIPv4Address,
				Description:  "Subnet mask associated with the destination.",
			},
		},
	}
}

func expandDMZ(d *schema.ResourceData, current *synclayer.DMZ) synclayer.DMZ {
	subnet := d.Get("subnet").(string)
	destination := d.Get("destination").(string)
	if current != nil {
		if subnet == "" {
			subnet = current.Subnet
		}
		if destination == "" {
			destination = current.Destination
		}
	}
	if subnet == "" {
		subnet = "255.255.255.0"
	}
	if destination == "" {
		destination = "0.0.0.0"
	}
	return synclayer.DMZ{
		Active:      d.Get("active").(bool),
		Destination: destination,
		Subnet:      subnet,
	}
}

func resourceDmzUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetDMZ(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := client.UpdateDMZ(ctx, expandDMZ(d, current)); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(dmzID)

	return resourceDmzRead(ctx, d, meta)
}

func resourceDmzRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	dmz, err := client.GetDMZ(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(dmzID)
	if err := d.Set("active", dmz.Active); err != nil {
		return diag.FromErr(err)
	}
	// A disabled DMZ is reported with destination 0.0.0.0; preserve the
	// configured value so that disabling does not cause perpetual diffs.
	if dmz.Active || d.Get("destination").(string) == "" {
		if err := d.Set("destination", dmz.Destination); err != nil {
			return diag.FromErr(err)
		}
	}
	if err := d.Set("subnet", dmz.Subnet); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourceDmzDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetDMZ(ctx)
	if err != nil {
		return diag.FromErr(err)
	}
	if current != nil {
		current.Active = false
		if err := client.UpdateDMZ(ctx, *current); err != nil {
			return diag.FromErr(err)
		}
	}
	d.SetId("")
	return nil
}
