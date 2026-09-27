package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// upnpID is the fixed id of this singleton resource.
const upnpID = "upnp"

func resourceUPnP() *schema.Resource {
	return &schema.Resource{
		Description: "Manages UPnP on the device.\n\n" +
			"The port mappings UPnP creates are made by clients and are not managed " +
			"here. Destroying this resource turns UPnP off and keeps the interval and " +
			"TTL. Import with the id `upnp`.",

		CreateContext: resourceUPnPUpsert,
		ReadContext:   resourceUPnPRead,
		UpdateContext: resourceUPnPUpsert,
		DeleteContext: resourceUPnPDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether UPnP is enabled.",
			},
			"interval": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				// The device rejects anything outside its own range; a floor
				// keeps obviously wrong values out of the API call.
				ValidateFunc: validation.IntAtLeast(1),
				Description:  "Announcement interval in seconds.",
			},
			"ttl": {
				Type:         schema.TypeInt,
				Optional:     true,
				Computed:     true,
				ValidateFunc: validation.IntAtLeast(1),
				Description:  "Time to live, in hops, of UPnP announcements.",
			},
		},
	}
}

func resourceUPnPUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	d.SetId(upnpID)

	// The device requires all three fields on every write (a write carrying
	// only `active` is refused), so the object is read first and the configured
	// fields are changed in it.
	current, err := client.GetUPnP(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	raw := d.GetRawConfig()
	if set, _ := attrConfigured(raw, "active"); set {
		current.Active = d.Get("active").(bool)
	}
	if set, _ := attrConfigured(raw, "interval"); set {
		current.Interval = d.Get("interval").(int)
	}
	if set, _ := attrConfigured(raw, "ttl"); set {
		current.TTL = d.Get("ttl").(int)
	}

	if err := client.UpdateUPnP(ctx, *current); err != nil {
		return diag.FromErr(err)
	}

	return resourceUPnPRead(ctx, d, meta)
}

func resourceUPnPRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetUPnP(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("active", current.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("interval", current.Interval); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ttl", current.TTL); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceUPnPDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	// UPnP has an obvious off state, but the interval and TTL are kept.
	current, err := client.GetUPnP(ctx)
	if err != nil {
		return diag.FromErr(err)
	}
	if current.Active {
		current.Active = false
		if err := client.UpdateUPnP(ctx, *current); err != nil {
			return diag.FromErr(err)
		}
	}

	d.SetId("")
	return nil
}
