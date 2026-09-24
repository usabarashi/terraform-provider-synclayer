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

func resourceStaticRoute() *schema.Resource {
	return &schema.Resource{
		Description: "Manages an IPv4 static route on the device.\n\n" +
			"Routes are identified by the numeric id assigned by the device; the " +
			"resource can be imported with that id.",

		CreateContext: resourceStaticRouteCreate,
		ReadContext:   resourceStaticRouteRead,
		UpdateContext: resourceStaticRouteUpdate,
		DeleteContext: resourceStaticRouteDelete,

		CustomizeDiff: customizeStaticRouteDiff,

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
				ValidateFunc: validation.IsIPv4Address,
				Description:  "Destination IPv4 network address.",
			},
			"subnet": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.IsIPv4Address,
				Description:  "Destination subnet mask, e.g. `255.255.255.0`.",
			},
			"gateway": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.IsIPv4Address,
				Description:  "Next-hop IPv4 address. Must be inside the LAN.",
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

// customizeStaticRouteDiff keeps the plan consistent with the normalisation
// performed on read: interface_name is always empty for LAN routes and is
// mandatory for WAN routes (so that the egress interface is unambiguous).
//
// Values that are not yet known (e.g. references to other resources) defer
// validation to apply time.
func customizeStaticRouteDiff(_ context.Context, d *schema.ResourceDiff, _ interface{}) error {
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

// validateStaticRoute enforces the interface_name/interface invariant at apply
// time, where values referenced from other resources are always known.
func validateStaticRoute(route synclayer.StaticRoute) error {
	if route.Interface != 0 && route.IfName == "" {
		return fmt.Errorf("interface_name is required when interface is \"wan\"")
	}
	return nil
}

func expandStaticRoute(d *schema.ResourceData) synclayer.StaticRoute {
	iface := 0
	ifName := ""
	// interface_name only applies to WAN routes; never send a (possibly stale)
	// WAN interface name together with Interface 0.
	if d.Get("interface").(string) == "wan" {
		iface = 1
		ifName = d.Get("interface_name").(string)
	}
	return synclayer.StaticRoute{
		Active:        d.Get("active").(bool),
		DestinationIP: d.Get("destination_ip").(string),
		Subnet:        d.Get("subnet").(string),
		Gateway:       d.Get("gateway").(string),
		Interface:     iface,
		IfName:        ifName,
	}
}

func flattenStaticRoute(d *schema.ResourceData, route *synclayer.StaticRoute) error {
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
	if err := d.Set("subnet", route.Subnet); err != nil {
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

func resourceStaticRouteCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	route := expandStaticRoute(d)
	if err := validateStaticRoute(route); err != nil {
		return diag.FromErr(err)
	}

	id, err := client.CreateStaticRoute(ctx, route)
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(strconv.Itoa(id))

	return resourceStaticRouteRead(ctx, d, meta)
}

func resourceStaticRouteRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid static route id %q: %s", d.Id(), err)
	}

	route, err := client.GetStaticRoute(ctx, id)
	if err != nil {
		return diag.FromErr(err)
	}
	if route == nil {
		d.SetId("")
		return nil
	}

	if err := flattenStaticRoute(d, route); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourceStaticRouteUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid static route id %q: %s", d.Id(), err)
	}

	route := expandStaticRoute(d)
	if err := validateStaticRoute(route); err != nil {
		return diag.FromErr(err)
	}

	newID, err := client.UpdateStaticRoute(ctx, id, route)
	if err != nil {
		return diag.FromErr(err)
	}
	// The firmware re-creates the route with a new id on update.
	d.SetId(strconv.Itoa(newID))
	return resourceStaticRouteRead(ctx, d, meta)
}

func resourceStaticRouteDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.Errorf("invalid static route id %q: %s", d.Id(), err)
	}

	if err := client.DeleteStaticRoute(ctx, id); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}
