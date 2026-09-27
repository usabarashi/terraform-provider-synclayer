package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

func resourcePacketFiltering() *schema.Resource {
	return &schema.Resource{
		Description: "Manages one packet filter rule.\n\n" +
			"Rules are addressed by an address family and a priority the device uses " +
			"as the rule's index. The device ships with a list of default rules in the " +
			"IPv4 family; they are not managed here, and creating a rule on a priority " +
			"that is already taken is refused rather than overwriting whatever is " +
			"there. Import with the id `<family>/<priority>`, e.g. `ipv4/20`.",

		CreateContext: resourcePacketFilteringCreate,
		ReadContext:   resourcePacketFilteringRead,
		UpdateContext: resourcePacketFilteringUpdate,
		DeleteContext: resourcePacketFilteringDelete,

		Importer: &schema.ResourceImporter{
			StateContext: importPacketFiltering,
		},

		Schema: map[string]*schema.Schema{
			"family": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringInSlice([]string{"ipv4", "ipv6"}, false),
				Description:  "Address family the rule lives in: `ipv4` or `ipv6`.",
			},
			"priority": {
				Type:     schema.TypeInt,
				Required: true,
				ForceNew: true,
				// The device stores the rule under this index and offers no way
				// to renumber rules, so a change here replaces the rule.
				ValidateFunc: validation.IntBetween(1, 32),
				Description:  "Position of the rule in the list, `1`-`32`. Changing it replaces the rule.",
			},
			"filter_type": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ValidateFunc: validation.StringInSlice([]string{"deny", "allow"}, false),
				Description:  "Whether matching traffic is dropped (`deny`) or admitted (`allow`).",
			},
			"target": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				// The device reports 0 and 1 for this and its web UI labels the
				// field "Target Interface"; the values are passed through
				// unchanged rather than guessed at.
				ValidateFunc: validation.IntBetween(0, 1),
				Description:  "Direction the rule applies to, as the device reports it (`0` or `1`; the web UI calls it the target interface).",
			},
			"protocol": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Protocol the rule matches, e.g. `tcp`, `udp` or `icmp`.",
			},
			"icmp_type": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "ICMP type when `protocol` is `icmp`.",
			},
			"description": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Free-form label the device stores with the rule.",
			},
			"src":  accessControlEndpointSchema("Source side of the rule."),
			"dest": accessControlEndpointSchema("Destination side of the rule."),
		},
	}
}

func accessControlEndpointSchema(description string) *schema.Schema {
	return &schema.Schema{
		Type:        schema.TypeList,
		Optional:    true,
		Computed:    true,
		MaxItems:    1,
		Description: description,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"type": {
					Type:        schema.TypeString,
					Optional:    true,
					Computed:    true,
					Description: "How the address is matched, e.g. `any`.",
				},
				"ip_address": {
					Type:        schema.TypeString,
					Optional:    true,
					Computed:    true,
					Description: "Address or network the side matches. The device keeps a placeholder here even when `type` is `any`.",
				},
				"start_port": {
					Type:        schema.TypeInt,
					Optional:    true,
					Computed:    true,
					Description: "First port of the range.",
				},
				"end_port": {
					Type:        schema.TypeInt,
					Optional:    true,
					Computed:    true,
					Description: "Last port of the range.",
				},
			},
		},
	}
}

func parsePacketFilteringID(id string) (string, int, error) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("expected id in the form <family>/<priority>, got %q", id)
	}
	family := parts[0]
	if family != "ipv4" && family != "ipv6" {
		return "", 0, fmt.Errorf("unsupported address family %q (expected \"ipv4\" or \"ipv6\")", family)
	}
	priority, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid priority %q: %w", parts[1], err)
	}
	return family, priority, nil
}

func importPacketFiltering(_ context.Context, d *schema.ResourceData, _ interface{}) ([]*schema.ResourceData, error) {
	family, priority, err := parsePacketFilteringID(d.Id())
	if err != nil {
		return nil, err
	}
	if err := d.Set("family", family); err != nil {
		return nil, err
	}
	if err := d.Set("priority", priority); err != nil {
		return nil, err
	}
	return []*schema.ResourceData{d}, nil
}

func expandPacketFiltering(d *schema.ResourceData) synclayer.AccessControlRule {
	return synclayer.AccessControlRule{
		// A rule the device holds is always active; it has no disabled state.
		Active:      true,
		Index:       d.Get("priority").(int),
		Description: d.Get("description").(string),
		FilterType:  d.Get("filter_type").(string),
		Target:      d.Get("target").(int),
		Protocol:    d.Get("protocol").(string),
		ICMPType:    d.Get("icmp_type").(string),
		Src:         expandAccessControlEndpoint(d.Get("src").([]interface{})),
		Dest:        expandAccessControlEndpoint(d.Get("dest").([]interface{})),
	}
}

func expandAccessControlEndpoint(raw []interface{}) synclayer.AccessControlEndpoint {
	if len(raw) == 0 || raw[0] == nil {
		return synclayer.AccessControlEndpoint{}
	}
	block := raw[0].(map[string]interface{})
	return synclayer.AccessControlEndpoint{
		Type:      block["type"].(string),
		IPAddress: block["ip_address"].(string),
		StartPort: block["start_port"].(int),
		EndPort:   block["end_port"].(int),
	}
}

func flattenAccessControlEndpoint(endpoint synclayer.AccessControlEndpoint) []interface{} {
	return []interface{}{map[string]interface{}{
		"type":       endpoint.Type,
		"ip_address": endpoint.IPAddress,
		"start_port": endpoint.StartPort,
		"end_port":   endpoint.EndPort,
	}}
}

func flattenPacketFiltering(d *schema.ResourceData, rule *synclayer.AccessControlRule, family string) error {
	d.SetId(fmt.Sprintf("%s/%d", family, rule.Index))

	if err := d.Set("family", family); err != nil {
		return err
	}
	if err := d.Set("priority", rule.Index); err != nil {
		return err
	}
	if err := d.Set("filter_type", rule.FilterType); err != nil {
		return err
	}
	if err := d.Set("target", rule.Target); err != nil {
		return err
	}
	if err := d.Set("protocol", rule.Protocol); err != nil {
		return err
	}
	if err := d.Set("icmp_type", rule.ICMPType); err != nil {
		return err
	}
	if err := d.Set("description", rule.Description); err != nil {
		return err
	}
	if err := d.Set("src", flattenAccessControlEndpoint(rule.Src)); err != nil {
		return err
	}
	if err := d.Set("dest", flattenAccessControlEndpoint(rule.Dest)); err != nil {
		return err
	}
	return nil
}

func resourcePacketFilteringCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	family := d.Get("family").(string)
	priority := d.Get("priority").(int)

	// Refuse a priority that is already in use *before* recording the resource.
	// The device would replace whatever sits there, and a resource that was
	// never created must not be able to delete a rule it does not own when it
	// is torn down.
	existing, err := client.GetAccessControlRule(ctx, family, priority)
	if err != nil {
		return diag.FromErr(err)
	}
	if existing != nil {
		return diag.Errorf("packet filter priority %d in %s is already used (by a rule described as %q): import it instead of taking it over", priority, family, existing.Description)
	}

	// From here the resource exists, so it is recorded before the write: a
	// failure part way through must leave something Terraform can track.
	d.SetId(fmt.Sprintf("%s/%d", family, priority))

	rule := expandPacketFiltering(d)
	if err := client.CreateAccessControlRule(ctx, family, rule); err != nil {
		// A failed create cannot be told apart from one that landed, and a rule
		// is deleted by priority alone, so the id is not kept: holding it could
		// later delete whatever ends up at that priority, including a rule this
		// configuration never created. A rule that did land has to be imported.
		d.SetId("")
		return diag.FromErr(err)
	}

	return resourcePacketFilteringRead(ctx, d, meta)
}

func resourcePacketFilteringRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	family, priority, err := parsePacketFilteringID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	rule, err := client.GetAccessControlRule(ctx, family, priority)
	if err != nil {
		return diag.FromErr(err)
	}
	if rule == nil {
		d.SetId("")
		return nil
	}

	if err := flattenPacketFiltering(d, rule, family); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourcePacketFilteringUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	family, priority, err := parsePacketFilteringID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	rule := expandPacketFiltering(d)
	if err := client.UpdateAccessControlRule(ctx, family, priority, rule); err != nil {
		return diag.FromErr(err)
	}
	return resourcePacketFilteringRead(ctx, d, meta)
}

func resourcePacketFilteringDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	family, priority, err := parsePacketFilteringID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	if err := client.DeleteAccessControlRule(ctx, family, priority); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
