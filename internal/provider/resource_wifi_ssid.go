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

func resourceWifiSSID() *schema.Resource {
	return &schema.Resource{
		Description: "Manages a single WiFi SSID (network) on the device.\n\n" +
			"SSIDs are identified by a band and an index: `2.4g`/`5g` and the SSID " +
			"slot reported by the device (0 is the primary network, higher indices " +
			"are guests). Existing slots cannot be created or removed; destroying " +
			"this resource disables the SSID instead. Import with the id " +
			"`<band>/<index>`, e.g. `5g/0`.",

		CreateContext: resourceWifiSSIDUpsert,
		ReadContext:   resourceWifiSSIDRead,
		UpdateContext: resourceWifiSSIDUpsert,
		DeleteContext: resourceWifiSSIDDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"band": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringInSlice([]string{"2.4g", "5g"}, false),
				Description:  "Radio band: `2.4g` or `5g`.",
			},
			"index": {
				Type:         schema.TypeInt,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.IntAtLeast(0),
				Description:  "SSID slot index on the band (0 is primary).",
			},
			"active": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether the SSID is enabled.",
			},
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Network name (SSID).",
			},
			"password": {
				Type:      schema.TypeString,
				Optional:  true,
				Sensitive: true,
				Description: "Pre-shared key. Stored in Terraform state; the device returns it as a " +
					"token-keyed envelope, so changes made outside Terraform are not detected.",
			},
			"security_type": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Security mode, e.g. `WPA2-PSK`, `WPA3-SAE` or `WPA3-SAE/WPA2-PSK`.",
			},
			"encryption": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Personal encryption cipher, e.g. `AES`.",
			},
			"group_key": {
				Type:        schema.TypeInt,
				Optional:    true,
				Computed:    true,
				Description: "Group key rekey interval in seconds.",
			},
			"mfp": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "Management frame protection: `capable`, `required` or `disabled`.",
			},
			"hidden_ssid": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether the SSID is hidden.",
			},
			"internet_only": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Restrict clients to internet access only.",
			},
			"ap_isolate": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Isolate wireless clients from each other (AP isolation).",
			},
			"web_ui_access": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Allow clients to reach the device web UI.",
			},
			"wmf": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Wireless multicast forwarding.",
			},
			"ft": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "802.11r fast transition.",
			},
			"access_control": {
				Type:        schema.TypeBool,
				Optional:    true,
				Computed:    true,
				Description: "Whether MAC access control is enabled for this SSID.",
			},
			"mac_address": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "BSSID of the SSID.",
			},
			"ssid_type": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Device classification of the slot (`primary` or `guest`).",
			},
		},
	}
}

func wifiBandID(name string) (int, error) {
	switch strings.ToLower(name) {
	case "2.4g":
		return 0, nil
	case "5g":
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported band %q (expected \"2.4g\" or \"5g\")", name)
	}
}

func wifiBandName(id int) string {
	if id == 1 {
		return "5g"
	}
	return "2.4g"
}

func parseWifiSSIDID(id string) (int, int, error) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected id in the form <band>/<index>, got %q", id)
	}
	band, err := wifiBandID(parts[0])
	if err != nil {
		return 0, 0, err
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid SSID index %q: %w", parts[1], err)
	}
	if index < 0 {
		return 0, 0, fmt.Errorf("invalid SSID index %d: must be >= 0", index)
	}
	return band, index, nil
}

func resourceWifiSSIDUpsert(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	band, index, err := parseWifiSSIDID(d.Id())
	if err != nil {
		// No id yet (create): derive from the configured band/index.
		var bandErr error
		band, bandErr = wifiBandID(d.Get("band").(string))
		if bandErr != nil {
			return diag.FromErr(bandErr)
		}
		index = d.Get("index").(int)
	}

	current, err := client.GetWifiSSID(ctx, band, index)
	if err != nil {
		return diag.FromErr(err)
	}
	if current == nil {
		return diag.Errorf("SSID %s/%d does not exist on the device", wifiBandName(band), index)
	}

	raw := d.GetRawConfig()

	// name is required, always apply it.
	current.Name = d.Get("name").(string)

	if set, _ := attrConfigured(raw, "active"); set {
		current.Active = d.Get("active").(bool)
	}
	if set, _ := attrConfigured(raw, "hidden_ssid"); set {
		current.HiddenSSID = d.Get("hidden_ssid").(bool)
	}
	if set, _ := attrConfigured(raw, "internet_only"); set {
		current.InternetOnly = d.Get("internet_only").(bool)
	}
	if set, _ := attrConfigured(raw, "ap_isolate"); set {
		current.APIsolate = d.Get("ap_isolate").(bool)
	}
	if set, _ := attrConfigured(raw, "web_ui_access"); set {
		current.WebUIAccess = d.Get("web_ui_access").(bool)
	}
	if set, _ := attrConfigured(raw, "wmf"); set {
		current.WMF = d.Get("wmf").(bool)
	}
	if set, _ := attrConfigured(raw, "ft"); set {
		current.FT = d.Get("ft").(bool)
	}
	if set, _ := attrConfigured(raw, "access_control"); set {
		current.AccessControl = d.Get("access_control").(bool)
	}
	if set, _ := attrConfigured(raw, "security_type"); set {
		current.Security.Type = d.Get("security_type").(string)
	}
	if set, _ := attrConfigured(raw, "mfp"); set {
		current.Security.MFP = d.Get("mfp").(string)
	}
	if set, _ := attrConfigured(raw, "encryption"); set {
		ensurePersonal(&current.Security).Encryption = d.Get("encryption").(string)
	}
	if set, _ := attrConfigured(raw, "group_key"); set {
		ensurePersonal(&current.Security).GroupKey = d.Get("group_key").(int)
	}

	password := d.Get("password").(string)
	if err := client.UpdateWifiSSID(ctx, band, index, *current, password); err != nil {
		return diag.FromErr(err)
	}

	d.SetId(fmt.Sprintf("%s/%d", wifiBandName(band), index))
	return resourceWifiSSIDRead(ctx, d, meta)
}

func ensurePersonal(security *synclayer.WifiSecurity) *synclayer.WifiSecurityPersonal {
	if security.Personal == nil {
		security.Personal = &synclayer.WifiSecurityPersonal{}
	}
	return security.Personal
}

func resourceWifiSSIDRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	band, index, err := parseWifiSSIDID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	ssid, err := client.GetWifiSSID(ctx, band, index)
	if err != nil {
		return diag.FromErr(err)
	}
	if ssid == nil {
		d.SetId("")
		return nil
	}

	if err := d.Set("band", wifiBandName(band)); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("index", index); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("active", ssid.Active); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("name", ssid.Name); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("security_type", ssid.Security.Type); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("mfp", ssid.Security.MFP); err != nil {
		return diag.FromErr(err)
	}
	if ssid.Security.Personal != nil {
		if err := d.Set("encryption", ssid.Security.Personal.Encryption); err != nil {
			return diag.FromErr(err)
		}
		if err := d.Set("group_key", ssid.Security.Personal.GroupKey); err != nil {
			return diag.FromErr(err)
		}
	} else {
		// The device dropped the personal block (e.g. the security mode changed
		// outside Terraform); clear the derived values so state does not keep
		// stale ones.
		if err := d.Set("encryption", ""); err != nil {
			return diag.FromErr(err)
		}
		if err := d.Set("group_key", 0); err != nil {
			return diag.FromErr(err)
		}
	}
	if err := d.Set("hidden_ssid", ssid.HiddenSSID); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("internet_only", ssid.InternetOnly); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ap_isolate", ssid.APIsolate); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("web_ui_access", ssid.WebUIAccess); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("wmf", ssid.WMF); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ft", ssid.FT); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("access_control", ssid.AccessControl); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("mac_address", ssid.MACAddress); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("ssid_type", ssid.Type); err != nil {
		return diag.FromErr(err)
	}

	// password is write-only and intentionally not read back.
	return nil
}

func resourceWifiSSIDDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client, diags := clientFromMeta(meta)
	if diags != nil {
		return diags
	}

	band, index, err := parseWifiSSIDID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	current, err := client.GetWifiSSID(ctx, band, index)
	if err != nil {
		return diag.FromErr(err)
	}
	if current != nil && current.Active {
		// SSID slots cannot be removed, so destroy disables the network.
		current.Active = false
		if err := client.UpdateWifiSSID(ctx, band, index, *current, ""); err != nil {
			return diag.FromErr(err)
		}
	}
	d.SetId("")
	return nil
}
