package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// lanID is the fixed id of this singleton resource.
const lanID = "lan"

func resourceLAN() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the LAN settings of the device: its DHCP server.\n\n" +
			"The LAN address and subnet are read-only here. Changing them moves the " +
			"address the provider and the rest of the network reach the device on, " +
			"which would leave an apply unable to finish, so they are read into state " +
			"and must be changed in the device web UI. Destroying this resource leaves " +
			"the device as it is. Import with the id `lan`.",

		CreateContext: resourceLANUpsert,
		ReadContext:   resourceLANRead,
		UpdateContext: resourceLANUpsert,
		DeleteContext: resourceLANDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"ip_address": {
				Type:     schema.TypeString,
				Computed: true,
				Description: "LAN address of the device. Read-only: change it in the device " +
					"web UI, where the change is made deliberately along with whatever " +
					"else depends on it.",
			},
			"subnet": {
				Type:     schema.TypeString,
				Computed: true,
				Description: "LAN subnet mask. Read-only, like `ip_address`; changing it " +
					"changes the whole LAN.",
			},
			"mac_address": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "MAC address of the LAN interface.",
			},
			"dhcp_active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether the DHCP server is enabled.",
			},
			"dhcp_start_ip": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "First address of the DHCP pool.",
			},
			"dhcp_end_ip": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Last address of the DHCP pool.",
			},
			"dhcp_lease_time": {
				Type:         schema.TypeInt,
				Optional:     true,
				Computed:     true,
				ValidateFunc: validation.IntAtLeast(1),
				Description:  "DHCP lease time in seconds (the web UI shows hours).",
			},
			"dhcp_wins_server": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "WINS server handed to DHCP clients.",
			},
			"dhcp_assignment": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "How addresses are assigned (`manual` selects from the registered addresses).",
			},
		},
	}
}

func resourceLANUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	d.SetId(lanID)

	// The device replaces the whole object, so it is read first and only the
	// DHCP fields are changed in it. The LAN address and subnet are never
	// written.
	current, err := client.GetLAN(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	raw := d.GetRawConfig()
	if set, _ := attrConfigured(raw, "dhcp_active"); set {
		current.IPv4.DHCP.Active = d.Get("dhcp_active").(bool)
	}
	if set, _ := attrConfigured(raw, "dhcp_start_ip"); set {
		current.IPv4.DHCP.StartIP = d.Get("dhcp_start_ip").(string)
	}
	if set, _ := attrConfigured(raw, "dhcp_end_ip"); set {
		current.IPv4.DHCP.EndIP = d.Get("dhcp_end_ip").(string)
	}
	if set, _ := attrConfigured(raw, "dhcp_lease_time"); set {
		current.IPv4.DHCP.LeaseTime = d.Get("dhcp_lease_time").(int)
	}
	if set, _ := attrConfigured(raw, "dhcp_wins_server"); set {
		current.IPv4.DHCP.WinsServer = d.Get("dhcp_wins_server").(string)
	}
	if set, _ := attrConfigured(raw, "dhcp_assignment"); set {
		current.IPv4.DHCP.Assignment = d.Get("dhcp_assignment").(string)
	}

	if err := client.UpdateLAN(ctx, *current); err != nil {
		return diag.FromErr(err)
	}

	return resourceLANRead(ctx, d, meta)
}

func resourceLANRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetLAN(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("ip_address", current.IPv4.IPAddress); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("subnet", current.IPv4.Subnet); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("mac_address", current.MACAddress); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("dhcp_active", current.IPv4.DHCP.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("dhcp_start_ip", current.IPv4.DHCP.StartIP); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("dhcp_end_ip", current.IPv4.DHCP.EndIP); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("dhcp_lease_time", current.IPv4.DHCP.LeaseTime); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("dhcp_wins_server", current.IPv4.DHCP.WinsServer); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("dhcp_assignment", current.IPv4.DHCP.Assignment); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceLANDelete(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
	// A LAN has no neutral setting to write back, and the address cannot be
	// changed safely from here, so only the Terraform record goes away.
	d.SetId("")
	return nil
}
