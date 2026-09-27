package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

func resourceStaticRouteIPv6() *schema.Resource {
	return &schema.Resource{
		Description: "Manages an IPv6 static route on the device.\n\n" +
			"Routes are identified by the numeric id assigned by the device; the " +
			"resource can be imported with that id. The IPv6 table takes a prefix " +
			"length where the IPv4 table takes a netmask.",

		CreateContext: resourceStaticRouteIPv6Create,
		ReadContext:   resourceStaticRouteIPv6Read,
		UpdateContext: resourceStaticRouteIPv6Update,
		DeleteContext: resourceStaticRouteIPv6Delete,

		CustomizeDiff: customizeStaticRouteIPv6Diff,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Whether the route is enabled.",
			},
			"destination_ip": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.IsIPv6Address,
				Description:  "Destination IPv6 network address.",
			},
			"prefix_length": {
				Type:     schema.TypeInt,
				Required: true,
				// A default route is "::" with a prefix length of 0, which the
				// device accepts.
				ValidateFunc: validation.IntBetween(0, 128),
				Description:  "Prefix length of the destination network, e.g. `64`. Use `0` for a default route.",
			},
			"gateway": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.IsIPv6Address,
				Description:  "Next-hop IPv6 address.",
			},
			"interface": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      "lan",
				ValidateFunc: validation.StringInSlice([]string{"lan", "wan"}, false),
				Description:  "Egress interface: `lan` or `wan`.",
			},
			"interface_name": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Concrete interface name (e.g. `veip0.1`). Required when `interface` is `wan`; must be omitted for `lan`.",
			},
		},
	}
}

// customizeStaticRouteIPv6Diff keeps the plan consistent with the
// normalisation performed on read, exactly as the IPv4 resource does.
func customizeStaticRouteIPv6Diff(_ context.Context, d *schema.ResourceDiff, _ interface{}) error {
	_, explicit, nameKnown := configuredAttr(d.GetRawConfig(), "interface_name")
	if !nameKnown || !d.NewValueKnown("interface") {
		return nil
	}

	iface := d.Get("interface").(string)

	if iface == "lan" {
		if explicit {
			return fmt.Errorf("interface_name is only valid when interface is \"wan\"")
		}
		return d.SetNew("interface_name", "")
	}

	if !explicit {
		return fmt.Errorf("interface_name is required when interface is \"wan\"")
	}
	return nil
}

func validateStaticRouteIPv6(route synclayer.StaticRouteIPv6) error {
	if route.Interface != 0 && route.IfName == "" {
		return fmt.Errorf("interface_name is required when interface is \"wan\"")
	}
	return nil
}

func expandStaticRouteIPv6(d *schema.ResourceData) synclayer.StaticRouteIPv6 {
	iface := 0
	ifName := ""
	// interface_name only applies to WAN routes; never send a (possibly stale)
	// WAN interface name together with Interface 0.
	if d.Get("interface").(string) == "wan" {
		iface = 1
		ifName = d.Get("interface_name").(string)
	}
	return synclayer.StaticRouteIPv6{
		Active:        d.Get("active").(bool),
		DestinationIP: d.Get("destination_ip").(string),
		PrefixLength:  d.Get("prefix_length").(int),
		Gateway:       d.Get("gateway").(string),
		Interface:     iface,
		IfName:        ifName,
	}
}

func flattenStaticRouteIPv6(d *schema.ResourceData, route *synclayer.StaticRouteIPv6) error {
	d.SetId(strconv.Itoa(route.ID))

	iface := "lan"
	if route.Interface != 0 {
		iface = "wan"
	}

	if err := d.Set("active", route.Active); err != nil {
		return err
	}
	if err := d.Set("destination_ip", route.DestinationIP); err != nil {
		return err
	}
	if err := d.Set("prefix_length", route.PrefixLength); err != nil {
		return err
	}
	if err := d.Set("gateway", route.Gateway); err != nil {
		return err
	}
	if err := d.Set("interface", iface); err != nil {
		return err
	}
	// The firmware reports "br0" for LAN routes; a LAN route must never retain
	// a WAN interface name, so always normalise it to the empty string.
	ifName := ""
	if route.Interface != 0 {
		ifName = route.IfName
	}
	if err := d.Set("interface_name", ifName); err != nil {
		return err
	}
	return nil
}

func resourceStaticRouteIPv6Create(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	route := expandStaticRouteIPv6(d)
	if err := validateStaticRouteIPv6(route); err != nil {
		return diag.FromErr(err)
	}

	id, err := client.CreateStaticRouteIPv6(ctx, route)
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(strconv.Itoa(id))

	return resourceStaticRouteIPv6Read(ctx, d, meta)
}

func resourceStaticRouteIPv6Read(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid static route id %q: %s", d.Id(), err)
	}

	route, err := client.GetStaticRouteIPv6(ctx, id)
	if err != nil {
		return diag.FromErr(err)
	}
	if route == nil {
		d.SetId("")
		return nil
	}

	if err := flattenStaticRouteIPv6(d, route); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourceStaticRouteIPv6Update(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid static route id %q: %s", d.Id(), err)
	}

	route := expandStaticRouteIPv6(d)
	if err := validateStaticRouteIPv6(route); err != nil {
		return diag.FromErr(err)
	}

	newID, err := client.UpdateStaticRouteIPv6(ctx, id, route)
	if err != nil {
		return diag.FromErr(err)
	}
	// The firmware re-creates the route with a new id on update.
	d.SetId(strconv.Itoa(newID))
	return resourceStaticRouteIPv6Read(ctx, d, meta)
}

func resourceStaticRouteIPv6Delete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid static route id %q: %s", d.Id(), err)
	}

	if err := client.DeleteStaticRouteIPv6(ctx, id); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
