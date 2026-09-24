package provider

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

var macAddressRegexp = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)

// New returns a fully configured Terraform provider.
//
// The provider is scoped to the manufacturer (SyncLayer); individual resources
// and data sources are namespaced by product (for example
// "synclayer_sxep200w_port_forwarding").
func New() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"host": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("SYNCLAYER_HOST", nil),
				Description: "Base URL of the device management interface, e.g. `http://192.168.0.1`. " +
					"The scheme may be omitted. Can also be set with the `SYNCLAYER_HOST` environment variable. " +
					"This argument is required.",
			},
			"username": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("SYNCLAYER_USERNAME", "admin"),
				Description: "Login ID used to authenticate against the device. " +
					"Can also be set with the `SYNCLAYER_USERNAME` environment variable.",
			},
			"password": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				DefaultFunc: schema.EnvDefaultFunc("SYNCLAYER_PASSWORD", nil),
				Description: "Login password used to authenticate against the device. " +
					"Can also be set with the `SYNCLAYER_PASSWORD` environment variable.",
			},
			"insecure": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Skip TLS certificate verification. Only needed when `host` uses `https`.",
			},
			"timeout": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     30,
				Description: "HTTP request timeout in seconds.",
			},
		},

		// The provider is scoped to the manufacturer (SyncLayer); individual
		// resources and data sources are namespaced by product (SXEP200W).
		ResourcesMap: map[string]*schema.Resource{
			"synclayer_sxep200w_port_forwarding": resourcePortForwarding(),
			"synclayer_sxep200w_static_route":    resourceStaticRoute(),
			"synclayer_sxep200w_reserved_ip":     resourceReservedIP(),
			"synclayer_sxep200w_ddns":            resourceDDNS(),
			"synclayer_sxep200w_dmz":             resourceDMZ(),
		},

		DataSourcesMap: map[string]*schema.Resource{
			"synclayer_sxep200w_system": dataSourceSystem(),
		},

		ConfigureContextFunc: configureProvider,
	}
}

func configureProvider(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	cfg := synclayer.Config{
		BaseURL:  d.Get("host").(string),
		Username: d.Get("username").(string),
		Password: d.Get("password").(string),
		Insecure: d.Get("insecure").(bool),
		Timeout:  time.Duration(d.Get("timeout").(int)) * time.Second,
	}

	client, err := synclayer.NewClient(cfg)
	if err != nil {
		return nil, diag.FromErr(err)
	}

	// Fail fast with an actionable message when the device is unreachable or
	// the credentials are wrong.
	if err := client.Login(ctx); err != nil {
		return nil, diag.Diagnostics{
			diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "Unable to authenticate with the SyncLayer device",
				Detail:   err.Error(),
			},
		}
	}

	return client, nil
}

func clientFromMeta(meta interface{}) (*synclayer.Client, diag.Diagnostics) {
	client, ok := meta.(*synclayer.Client)
	if !ok || client == nil {
		return nil, diag.Errorf("provider is not configured; ensure the provider block is present and valid")
	}
	return client, nil
}
