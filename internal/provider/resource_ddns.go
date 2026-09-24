package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

const ddnsID = "ddns"

func resourceDDNS() *schema.Resource {
	return &schema.Resource{
		Description: "Manages the dynamic DNS (DDNS) client of the device.\n\n" +
			"This is a singleton resource: there is only one DDNS configuration per " +
			"device. Destroying the resource disables DDNS instead of deleting the " +
			"configuration. Import it with the id `ddns`.",

		CreateContext: resourceDdnsUpsert,
		ReadContext:   resourceDdnsRead,
		UpdateContext: resourceDdnsUpsert,
		DeleteContext: resourceDdnsDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Whether the DDNS client is enabled.",
			},
			"provider_name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "DDNS provider name as reported by the device, e.g. `DynDNS`, `NoIP` or `User Define`.",
			},
			"username": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Account username (or token, depending on the provider).",
			},
			"password": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "Account password. Write-only: it is never read back from the device.",
			},
			"token": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "Account token, when the provider uses token authentication.",
			},
			"hostname": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Hostname to register with the provider.",
			},
			"url": {
				Type:     schema.TypeString,
				Optional: true,
				Description: "Update URL to request. When omitted, the URL advertised by the selected provider is used. " +
					"This value is authoritative for the `User Define` provider (drift is detected and restored). " +
					"The built-in DynDNS/NoIP providers ignore it and use their own endpoint; observe `resolved_url` " +
					"for the effective value.",
			},
			"resolved_url": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Effective update URL in use on the device (the configured `url` or the provider default).",
			},
			"connection_status": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Last connection status reported by the device.",
			},
			"ip_address": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Last IP address reported by the device.",
			},
		},
	}
}

func resourceDdnsUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	current, err := client.GetDDNS(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	providerName := d.Get("provider_name").(string)
	index := -1
	for i, p := range current.SupportingProvider {
		if p.Name == providerName {
			index = i
			break
		}
	}
	if index < 0 {
		return diag.Errorf("unknown DDNS provider %q; the device supports %s", providerName, ddnsProviderNames(current))
	}

	// url is optional; when omitted the provider default is used. It is never
	// written back to state, so removing it from the configuration cleanly
	// falls back to the default on the next apply.
	url := d.Get("url").(string)
	if url == "" {
		url = current.SupportingProvider[index].URL
	}

	cfg := synclayer.DDNSConfiguration{
		CurrentProvider: index,
		Username:        d.Get("username").(string),
		Password:        d.Get("password").(string),
		Token:           d.Get("token").(string),
		Hostname:        d.Get("hostname").(string),
		URL:             url,
	}

	result, err := client.UpdateDDNS(ctx, d.Get("active").(bool), cfg)
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(ddnsID)

	if result != nil {
		if err := d.Set("connection_status", result.ConnectionStatus); err != nil {
			return diag.FromErr(err)
		}
		if err := d.Set("ip_address", result.IPAddress); err != nil {
			return diag.FromErr(err)
		}
	}

	return resourceDdnsRead(ctx, d, meta)
}

func resourceDdnsRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	ddns, err := client.GetDDNS(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(ddnsID)

	if err := d.Set("active", ddns.Active); err != nil {
		return diag.FromErr(err)
	}
	providerName := ""
	if idx := ddns.Configuration.CurrentProvider; idx >= 0 && idx < len(ddns.SupportingProvider) {
		providerName = ddns.SupportingProvider[idx].Name
		if err := d.Set("provider_name", providerName); err != nil {
			return diag.FromErr(err)
		}
	}
	if err := d.Set("username", ddns.Configuration.Username); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("token", ddns.Configuration.Token); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("hostname", ddns.Configuration.Hostname); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("resolved_url", ddns.Configuration.URL); err != nil {
		return diag.FromErr(err)
	}
	// The custom url is only authoritative for the "User Define" provider; the
	// built-in providers ignore it and use their own endpoint. Reconcile it so
	// that an out-of-band change is detected as drift and restored on the next
	// apply.
	if providerName == "User Define" {
		if configured := d.Get("url").(string); configured != "" {
			if err := d.Set("url", ddns.Configuration.URL); err != nil {
				return diag.FromErr(err)
			}
		}
	}
	if err := d.Set("connection_status", ddns.Result.ConnectionStatus); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ip_address", ddns.Result.IPAddress); err != nil {
		return diag.FromErr(err)
	}

	// password is intentionally not read back: the device returns it encrypted
	// and it is only meaningful on write.
	return nil
}

func resourceDdnsDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	// Disabling DDNS is the closest thing to deletion this device offers. The
	// configuration is cleared; that is acceptable for a destroy operation.
	if _, err := client.UpdateDDNS(ctx, false, synclayer.DDNSConfiguration{}); err != nil {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func ddnsProviderNames(ddns *synclayer.DDNS) string {
	names := make([]string, 0, len(ddns.SupportingProvider))
	for _, p := range ddns.SupportingProvider {
		names = append(names, fmt.Sprintf("%q", p.Name))
	}
	return strings.Join(names, ", ")
}
