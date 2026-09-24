package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

func resourcePortForwarding() *schema.Resource {
	return &schema.Resource{
		Description: "Manages a port forwarding (NAPT) rule on the device.\n\n" +
			"Rules are identified by the numeric id assigned by the device; the " +
			"resource can be imported with that id.",

		CreateContext: resourcePortForwardingCreate,
		ReadContext:   resourcePortForwardingRead,
		UpdateContext: resourcePortForwardingUpdate,
		DeleteContext: resourcePortForwardingDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Whether the rule is enabled.",
			},
			"service_type": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Free-form label for the rule, e.g. `HTTP` or `SSH`.",
			},
			"ip_address": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.IsIPv4Address,
				Description:  "LAN IPv4 address that receives the forwarded traffic.",
			},
			"protocol": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringInSlice([]string{"tcp", "udp", "tcp/udp"}, false),
				Description:  "Transport protocol: `tcp`, `udp` or `tcp/udp`.",
			},
			"local_port_start": {
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: validation.IntBetween(1, 65535),
				Description:  "First port of the destination (LAN) range.",
			},
			"local_port_end": {
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: validation.IntBetween(1, 65535),
				Description:  "Last port of the destination (LAN) range.",
			},
			"external_port_start": {
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: validation.IntBetween(1, 65535),
				Description:  "First port of the source (WAN) range.",
			},
			"external_port_end": {
				Type:         schema.TypeInt,
				Required:     true,
				ValidateFunc: validation.IntBetween(1, 65535),
				Description:  "Last port of the source (WAN) range.",
			},
		},
	}
}

func expandPortForwarding(d *schema.ResourceData) synclayer.PortForwardingRule {
	return synclayer.PortForwardingRule{
		Active:      d.Get("active").(bool),
		ServiceType: d.Get("service_type").(string),
		IPAddress:   d.Get("ip_address").(string),
		Protocol:    d.Get("protocol").(string),
		LocalPort: synclayer.PortRange{
			Start: d.Get("local_port_start").(int),
			End:   d.Get("local_port_end").(int),
		},
		ExternalPort: synclayer.PortRange{
			Start: d.Get("external_port_start").(int),
			End:   d.Get("external_port_end").(int),
		},
	}
}

func flattenPortForwarding(d *schema.ResourceData, rule *synclayer.PortForwardingRule) error {
	d.SetId(strconv.Itoa(rule.ID))
	if err := d.Set("active", rule.Active); err != nil {
		return err
	}
	if err := d.Set("service_type", rule.ServiceType); err != nil {
		return err
	}
	if err := d.Set("ip_address", rule.IPAddress); err != nil {
		return err
	}
	if err := d.Set("protocol", rule.Protocol); err != nil {
		return err
	}
	if err := d.Set("local_port_start", rule.LocalPort.Start); err != nil {
		return err
	}
	if err := d.Set("local_port_end", rule.LocalPort.End); err != nil {
		return err
	}
	if err := d.Set("external_port_start", rule.ExternalPort.Start); err != nil {
		return err
	}
	if err := d.Set("external_port_end", rule.ExternalPort.End); err != nil {
		return err
	}
	return nil
}

func resourcePortForwardingCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := client.CreatePortForwarding(ctx, expandPortForwarding(d))
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(strconv.Itoa(id))

	return resourcePortForwardingRead(ctx, d, meta)
}

func resourcePortForwardingRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid port forwarding id %q: %s", d.Id(), err)
	}

	rule, err := client.GetPortForwarding(ctx, id)
	if err != nil {
		return diag.FromErr(err)
	}
	if rule == nil {
		// The rule no longer exists on the device.
		d.SetId("")
		return nil
	}

	if err := flattenPortForwarding(d, rule); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourcePortForwardingUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid port forwarding id %q: %s", d.Id(), err)
	}

	if err := client.UpdatePortForwarding(ctx, id, expandPortForwarding(d)); err != nil {
		return diag.FromErr(err)
	}
	return resourcePortForwardingRead(ctx, d, meta)
}

func resourcePortForwardingDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid port forwarding id %q: %s", d.Id(), err)
	}

	if err := client.DeletePortForwarding(ctx, id); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
