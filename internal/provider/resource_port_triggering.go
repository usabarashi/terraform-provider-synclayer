package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

func resourcePortTriggering() *schema.Resource {
	return &schema.Resource{
		Description: "Manages one port triggering rule.\n\n" +
			"A rule opens a forwarded range while traffic is seen on its triggered " +
			"range. Rules are identified by the id the device assigns; the resource " +
			"can be imported with that id. The switch that turns port triggering on " +
			"and off for the whole feature is not managed here.",

		CreateContext: resourcePortTriggeringCreate,
		ReadContext:   resourcePortTriggeringRead,
		UpdateContext: resourcePortTriggeringUpdate,
		DeleteContext: resourcePortTriggeringDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Whether the rule is enabled. A disabled rule stays in the device's list.",
			},
			"description": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Free-form label the device stores with the rule.",
			},
			"triggered": portTriggeringRangeSchema(
				"Ports whose traffic arms the rule.",
				"Protocol the triggered range matches, e.g. `tcp`, `udp` or `tcp/udp`.",
			),
			"forwarded": portTriggeringRangeSchema(
				"Ports opened while the rule is armed.",
				"Protocol the forwarded range matches.",
			),
		},
	}
}

func portTriggeringRangeSchema(description, protocolDescription string) *schema.Schema {
	return &schema.Schema{
		Type:        schema.TypeList,
		Required:    true,
		MaxItems:    1,
		Description: description,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"protocol": {
					Type:        schema.TypeString,
					Optional:    true,
					Default:     "tcp",
					Description: protocolDescription,
				},
				"start_range": {
					Type:         schema.TypeInt,
					Required:     true,
					ValidateFunc: validation.IntBetween(1, 65535),
					Description:  "First port of the range.",
				},
				"end_range": {
					Type:         schema.TypeInt,
					Required:     true,
					ValidateFunc: validation.IntBetween(1, 65535),
					Description:  "Last port of the range.",
				},
			},
		},
	}
}

func expandPortTriggeringRange(raw []interface{}) synclayer.PortTriggeringRange {
	if len(raw) == 0 || raw[0] == nil {
		return synclayer.PortTriggeringRange{}
	}
	block := raw[0].(map[string]interface{})
	return synclayer.PortTriggeringRange{
		Protocol:   block["protocol"].(string),
		StartRange: block["start_range"].(int),
		EndRange:   block["end_range"].(int),
	}
}

func flattenPortTriggeringRange(value synclayer.PortTriggeringRange) []interface{} {
	return []interface{}{map[string]interface{}{
		"protocol":    value.Protocol,
		"start_range": value.StartRange,
		"end_range":   value.EndRange,
	}}
}

func expandPortTriggering(d *schema.ResourceData) synclayer.PortTriggeringRule {
	return synclayer.PortTriggeringRule{
		Active:      d.Get("active").(bool),
		Description: d.Get("description").(string),
		Triggered:   expandPortTriggeringRange(d.Get("triggered").([]interface{})),
		Forwarded:   expandPortTriggeringRange(d.Get("forwarded").([]interface{})),
	}
}

func flattenPortTriggering(d *schema.ResourceData, rule *synclayer.PortTriggeringRule) error {
	d.SetId(strconv.Itoa(rule.ID))

	if err := d.Set("active", rule.Active); err != nil {
		return err
	}
	if err := d.Set("description", rule.Description); err != nil {
		return err
	}
	if err := d.Set("triggered", flattenPortTriggeringRange(rule.Triggered)); err != nil {
		return err
	}
	if err := d.Set("forwarded", flattenPortTriggeringRange(rule.Forwarded)); err != nil {
		return err
	}
	return nil
}

func resourcePortTriggeringCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	rule := expandPortTriggering(d)
	id, err := client.CreatePortTriggeringRule(ctx, rule)
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(strconv.Itoa(id))

	return resourcePortTriggeringRead(ctx, d, meta)
}

func resourcePortTriggeringRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid port triggering rule id %q: %s", d.Id(), err)
	}

	rule, err := client.GetPortTriggeringRule(ctx, id)
	if err != nil {
		return diag.FromErr(err)
	}
	if rule == nil {
		d.SetId("")
		return nil
	}

	if err := flattenPortTriggering(d, rule); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourcePortTriggeringUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid port triggering rule id %q: %s", d.Id(), err)
	}

	rule := expandPortTriggering(d)
	if err := client.UpdatePortTriggeringRule(ctx, id, rule); err != nil {
		return diag.FromErr(err)
	}
	return resourcePortTriggeringRead(ctx, d, meta)
}

func resourcePortTriggeringDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid port triggering rule id %q: %s", d.Id(), err)
	}

	if err := client.DeletePortTriggeringRule(ctx, id); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
