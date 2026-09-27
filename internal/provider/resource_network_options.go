package provider

import (
	"context"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

// networkOptionsID is the fixed id of this singleton resource.
const networkOptionsID = "network_options"

func resourceNetworkOptions() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the device-wide network options the web UI groups under " +
			"Advanced > Network, together with the ALG helpers that share that page.\n\n" +
			"Each is a single value on the device, so they share one resource. The " +
			"device replaces the whole options object on every write, and refuses a " +
			"partial one, so the provider always reads the object before changing it. " +
			"Destroying this resource removes it from state and leaves the device as it " +
			"is: these options have no neutral value — clearing a blocking flag loosens " +
			"traffic, clearing a passthrough flag restricts it. Import with the id " +
			"`network_options`.",

		CreateContext: resourceNetworkOptionsUpsert,
		ReadContext:   resourceNetworkOptionsRead,
		UpdateContext: resourceNetworkOptionsUpsert,
		DeleteContext: resourceNetworkOptionsDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"wan_icmpv4_blocking": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Block ICMPv4 (ping) from the WAN side.",
			},
			"wan_icmpv6_blocking": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Block ICMPv6 from the WAN side.",
			},
			"ipsec_passthrough": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Let IPsec traffic pass through to clients.",
			},
			"pptp_passthrough": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Let PPTP traffic pass through to clients.",
			},
			"l2tp_passthrough": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Let L2TP traffic pass through to clients.",
			},
			"nat_tcp_timer": {
				Type:        schema.TypeInt,
				Optional:    true,
				Computed:    true,
				Description: "TCP session timeout of the NAT/SPI table, in seconds.",
			},
			"nat_udp_timer": {
				Type:        schema.TypeInt,
				Optional:    true,
				Computed:    true,
				Description: "UDP session timeout of the NAT/SPI table, in seconds.",
			},
			"remote_access_enabled": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Offer management from the WAN side. Enable deliberately: it exposes the device web UI beyond the LAN.",
			},
			"secure_access_enabled": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Offer secure (HTTPS) management from the WAN side.",
			},
			"management_port": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				// The device clamps rather than rejects: a write of 65536 came
				// back as 65535 (verified on VER-01.06.05-EA), so out-of-range
				// values are refused here instead of being silently changed.
				ValidateFunc: validation.IntBetween(1, 65535),
				Description: "Port the device offers WAN-side management on. It keeps one " +
					"port for both the remote and the secure service, and changing it " +
					"restarts the device web service, which drops the connection that is " +
					"writing it.",
			},
			"multicast": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Allow multicast through the device.",
			},
			"alg_enabled": {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				// Service codes are hashed case-insensitively, so a lower-case
				// configuration matches what the device reports.
				Set: func(v interface{}) int {
					return schema.HashString(strings.ToUpper(v.(string)))
				},
				Elem: &schema.Schema{
					Type:         schema.TypeString,
					ValidateFunc: validation.StringIsNotEmpty,
				},
				// The device replaces the whole helper list on every write: an
				// entry left out is switched off, so this is the complete set of
				// enabled helpers rather than a set of additions (verified on
				// VER-01.06.05-EA).
				Description: "Application-layer gateway helpers to leave enabled, by service " +
					"code (`ftp`, `sip`, ...). This is the complete set: a helper the " +
					"device offers but this list omits is switched off, which shows up " +
					"as a difference on the next plan.",
			},
		},
	}
}

func resourceNetworkOptionsUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	// Record the resource before the first write: this resource writes two
	// endpoints, and a failure in the second would otherwise leave the first
	// applied but untracked.
	d.SetId(networkOptionsID)

	// Every option is one value the device keeps, so the write always replays
	// the object it read.
	current, err := client.GetNetworkOption(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	raw := d.GetRawConfig()
	if set, _ := attrConfigured(raw, "wan_icmpv4_blocking"); set {
		current.WANBlocking = d.Get("wan_icmpv4_blocking").(bool)
	}
	if set, _ := attrConfigured(raw, "wan_icmpv6_blocking"); set {
		current.ICMPv6Blocking = d.Get("wan_icmpv6_blocking").(bool)
	}
	if set, _ := attrConfigured(raw, "ipsec_passthrough"); set {
		current.IPSecPassthrough = d.Get("ipsec_passthrough").(bool)
	}
	if set, _ := attrConfigured(raw, "pptp_passthrough"); set {
		current.PPTPPassthrough = d.Get("pptp_passthrough").(bool)
	}
	if set, _ := attrConfigured(raw, "l2tp_passthrough"); set {
		current.L2TPPassthrough = d.Get("l2tp_passthrough").(bool)
	}
	if set, _ := attrConfigured(raw, "nat_tcp_timer"); set {
		current.NATTCPTimer = d.Get("nat_tcp_timer").(int)
	}
	if set, _ := attrConfigured(raw, "nat_udp_timer"); set {
		current.NATUDPTimer = d.Get("nat_udp_timer").(int)
	}
	if set, _ := attrConfigured(raw, "remote_access_enabled"); set {
		current.RemoteAccess.Active = d.Get("remote_access_enabled").(bool)
	}
	if set, _ := attrConfigured(raw, "secure_access_enabled"); set {
		current.SecureAccess.Active = d.Get("secure_access_enabled").(bool)
	}
	if set, _ := attrConfigured(raw, "management_port"); set {
		current.RemoteAccess.Port = d.Get("management_port").(int)
		current.SecureAccess.Port = d.Get("management_port").(int)
	}
	if set, _ := attrConfigured(raw, "multicast"); set {
		current.Multicast = d.Get("multicast").(bool)
	}

	if err := client.UpdateNetworkOption(ctx, *current); err != nil {
		// Changing the management port restarts the device web service, so the
		// write can come back as a dropped connection even though it was
		// applied. A fresh read decides which happened.
		after, readErr := client.GetNetworkOption(ctx)
		if readErr != nil || after == nil || *after != *current {
			return diag.FromErr(err)
		}
	}

	// The ALG helpers live behind their own endpoint and are written only when
	// they are configured: an unrelated options change must not disturb them.
	if set, _ := attrConfigured(raw, "alg_enabled"); set {
		entries, err := client.ListALG(ctx)
		if err != nil {
			return diag.FromErr(err)
		}

		offered := make(map[string]bool, len(entries))
		for _, entry := range entries {
			offered[strings.ToUpper(entry.ServiceCode)] = true
		}

		wanted := make(map[string]bool, len(entries))
		for _, v := range d.Get("alg_enabled").(*schema.Set).List() {
			code := strings.ToUpper(v.(string))
			// A code the device does not offer would be dropped by the write
			// below, leaving a configuration that never converges, so it is
			// refused instead.
			if !offered[code] {
				return diag.Errorf("unknown ALG service code %q: this device offers %s", v.(string), strings.Join(algServiceCodes(entries), ", "))
			}
			wanted[code] = true
		}

		// The configured set is the complete set of enabled helpers: the device
		// switches off anything left out, so every entry is written explicitly.
		for i := range entries {
			entries[i].Active = wanted[strings.ToUpper(entries[i].ServiceCode)]
		}
		if err := client.UpdateALG(ctx, entries); err != nil {
			return diag.FromErr(err)
		}
	}

	return resourceNetworkOptionsRead(ctx, d, meta)
}

func resourceNetworkOptionsRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetNetworkOption(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("wan_icmpv4_blocking", current.WANBlocking); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("wan_icmpv6_blocking", current.ICMPv6Blocking); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ipsec_passthrough", current.IPSecPassthrough); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("pptp_passthrough", current.PPTPPassthrough); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("l2tp_passthrough", current.L2TPPassthrough); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("nat_tcp_timer", current.NATTCPTimer); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("nat_udp_timer", current.NATUDPTimer); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("remote_access_enabled", current.RemoteAccess.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("secure_access_enabled", current.SecureAccess.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("management_port", current.RemoteAccess.Port); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("multicast", current.Multicast); err != nil {
		return diag.FromErr(err)
	}

	entries, err := client.ListALG(ctx)
	if err != nil {
		return diag.FromErr(err)
	}
	enabled := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Active {
			enabled = append(enabled, entry.ServiceCode)
		}
	}
	if err := d.Set("alg_enabled", enabled); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceNetworkOptionsDelete(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
	// There is no neutral value to write back: the device keeps whatever it
	// has, and only the Terraform record goes away.
	d.SetId("")
	return nil
}

// algServiceCodes lists the helper codes the device offers, sorted, for error
// messages.
func algServiceCodes(entries []synclayer.ALGEntry) []string {
	codes := make([]string, 0, len(entries))
	for _, entry := range entries {
		codes = append(codes, entry.ServiceCode)
	}
	sort.Strings(codes)
	return codes
}
