package provider

import (
	"context"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

func resourceReservedIP() *schema.Resource {
	return &schema.Resource{
		Description: "Manages a DHCP reserved IP address (static lease) on the device.\n\n" +
			"Reservations are identified by the numeric id assigned by the device; " +
			"the resource can be imported with that id.",

		CreateContext: resourceReservedIPCreate,
		ReadContext:   resourceReservedIPRead,
		UpdateContext: resourceReservedIPUpdate,
		DeleteContext: resourceReservedIPDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"mac_address": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringMatch(macAddressRegexp, "must be a MAC address such as AA:BB:CC:DD:EE:FF"),
				// The device may normalise the case of the MAC address, and a
				// case-only change does not identify a different client.
				DiffSuppressFunc: func(_, old, new string, _ *schema.ResourceData) bool {
					return strings.EqualFold(old, new)
				},
				Description: "MAC address of the client. Changing this forces a new reservation.",
			},
			"ip_address": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.IsIPAddress,
				Description:  "IPv4 address to assign to the client.",
			},
			"device_name": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Human readable label for the client. May be normalised by the device.",
			},
		},
	}
}

func expandReservedIP(d *schema.ResourceData) synclayer.ReservedIP {
	return synclayer.ReservedIP{
		MacAddress: d.Get("mac_address").(string),
		IPAddress:  d.Get("ip_address").(string),
		DeviceName: d.Get("device_name").(string),
	}
}

func flattenReservedIP(d *schema.ResourceData, rule *synclayer.ReservedIP) error {
	d.SetId(strconv.Itoa(rule.ID))
	if err := d.Set("mac_address", rule.MacAddress); err != nil {
		return err
	}
	if err := d.Set("ip_address", rule.IPAddress); err != nil {
		return err
	}
	if err := d.Set("device_name", rule.DeviceName); err != nil {
		return err
	}
	return nil
}

func resourceReservedIPCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := client.CreateReservedIP(ctx, expandReservedIP(d))
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(strconv.Itoa(id))

	return resourceReservedIPRead(ctx, d, meta)
}

func resourceReservedIPRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid reserved IP id %q: %s", d.Id(), err)
	}

	rule, err := client.GetReservedIP(ctx, id)
	if err != nil {
		return diag.FromErr(err)
	}
	if rule == nil {
		d.SetId("")
		return nil
	}

	if err := flattenReservedIP(d, rule); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourceReservedIPUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid reserved IP id %q: %s", d.Id(), err)
	}

	if err := client.UpdateReservedIP(ctx, id, expandReservedIP(d)); err != nil {
		return diag.FromErr(err)
	}
	return resourceReservedIPRead(ctx, d, meta)
}

func resourceReservedIPDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid reserved IP id %q: %s", d.Id(), err)
	}

	if err := client.DeleteReservedIP(ctx, id); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
