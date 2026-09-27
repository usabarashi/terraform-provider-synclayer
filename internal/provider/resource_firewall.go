package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// firewallID is the fixed id of this singleton resource.
const firewallID = "firewall"

func resourceFirewall() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the device firewall.\n\n" +
			"The device reports the same settings through an aggregate document and " +
			"through per-family endpoints, so this resource is the single owner of " +
			"both. Destroying it removes it from state and leaves the firewall as it " +
			"is: switching protection off is not a neutral default. Import with the id " +
			"`firewall`.",

		CreateContext: resourceFirewallUpsert,
		ReadContext:   resourceFirewallRead,
		UpdateContext: resourceFirewallUpsert,
		DeleteContext: resourceFirewallDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"ipv4_active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether the IPv4 firewall is enabled.",
			},
			"ipv4_level": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				// The levels select one of the device's rule banks; their rules
				// are read-only and are not exposed here.
				ValidateFunc: validation.StringInSlice([]string{"low", "medium", "high"}, false),
				Description:  "IPv4 firewall level, which selects a bank of preset rules: `low`, `medium` or `high`.",
			},
			"ipv4_block_fragmented_ip_packets": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Drop fragmented IPv4 packets.",
			},
			"ipv4_block_port_scan_detection": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Detect and drop IPv4 port scans.",
			},
			"ipv4_block_ip_flood": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Drop IPv4 flood traffic.",
			},
			"ipv4_block_ip_spoofing": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Drop IPv4 packets with a spoofed source address.",
			},
			"ipv6_active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether the IPv6 firewall is enabled.",
			},
		},
	}
}

func resourceFirewallUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	d.SetId(firewallID)

	// The write replaces the whole object, so it starts from what the device
	// reports and only changes what is configured.
	current, err := client.GetFirewall(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	raw := d.GetRawConfig()
	if set, _ := attrConfigured(raw, "ipv4_active"); set {
		current.IPv4.Active = d.Get("ipv4_active").(bool)
	}
	if set, _ := attrConfigured(raw, "ipv4_level"); set {
		current.IPv4.Level = d.Get("ipv4_level").(string)
	}
	if set, _ := attrConfigured(raw, "ipv4_block_fragmented_ip_packets"); set {
		current.IPv4.Blocks.FragmentedIPPackets = d.Get("ipv4_block_fragmented_ip_packets").(bool)
	}
	if set, _ := attrConfigured(raw, "ipv4_block_port_scan_detection"); set {
		current.IPv4.Blocks.PortScanDetection = d.Get("ipv4_block_port_scan_detection").(bool)
	}
	if set, _ := attrConfigured(raw, "ipv4_block_ip_flood"); set {
		current.IPv4.Blocks.IPFlood = d.Get("ipv4_block_ip_flood").(bool)
	}
	if set, _ := attrConfigured(raw, "ipv4_block_ip_spoofing"); set {
		current.IPv4.Blocks.IPSpoofing = d.Get("ipv4_block_ip_spoofing").(bool)
	}
	if set, _ := attrConfigured(raw, "ipv6_active"); set {
		current.IPv6.Active = d.Get("ipv6_active").(bool)
	}

	if err := client.UpdateFirewall(ctx, *current); err != nil {
		return diag.FromErr(err)
	}

	return resourceFirewallRead(ctx, d, meta)
}

func resourceFirewallRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetFirewall(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("ipv4_active", current.IPv4.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ipv4_level", current.IPv4.Level); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ipv4_block_fragmented_ip_packets", current.IPv4.Blocks.FragmentedIPPackets); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ipv4_block_port_scan_detection", current.IPv4.Blocks.PortScanDetection); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ipv4_block_ip_flood", current.IPv4.Blocks.IPFlood); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ipv4_block_ip_spoofing", current.IPv4.Blocks.IPSpoofing); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ipv6_active", current.IPv6.Active); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceFirewallDelete(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
	// Switching the firewall off is not a neutral default, so nothing is
	// written back; only the Terraform record goes away.
	d.SetId("")
	return nil
}
