package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func dataSourceSystem() *schema.Resource {
	return &schema.Resource{
		Description: "Reads static information about the device (model, serial number, firmware version).",

		ReadContext: dataSourceSystemRead,

		Schema: map[string]*schema.Schema{
			"model_name": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Device model, e.g. `SXEP200W`.",
			},
			"serial_no": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Device serial number.",
			},
			"hardware_version": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Hardware revision.",
			},
			"software_version": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Firmware version.",
			},
			"build_time": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Firmware build timestamp.",
			},
			"operation_time": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Uptime as reported by the device.",
			},
			"base_mac": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Base MAC address of the device.",
			},
		},
	}
}

func dataSourceSystemRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	about, err := client.About(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(about.SerialNo)
	if d.Id() == "" {
		d.SetId(about.ModelName)
	}

	if err := d.Set("model_name", about.ModelName); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("serial_no", about.SerialNo); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("hardware_version", about.Hardware.Version); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("software_version", about.Software.Version); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("build_time", about.Software.BuildTime); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("operation_time", about.OperationTime); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("base_mac", about.BaseMAC); err != nil {
		return diag.FromErr(err)
	}
	return nil
}
