package provider

import (
	"context"
	"fmt"
	"sort"
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
			StateContext: importWifiSSID,
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
				Type:     schema.TypeInt,
				Required: true,
				ForceNew: true,
				// Only the primary (0) and secondary (1) slots are exposed by the
				// device web UI. Some firmware revisions report additional slots
				// (e.g. 5g/2), but the UI cannot operate them, so managing them
				// here is refused.
				ValidateFunc: validation.IntInSlice([]int{0, 1}),
				Description:  "SSID slot index on the band: `0` (primary) or `1` (secondary).",
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
			"access_control_allow": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,
				Description: "How `access_control_rules` are interpreted: `true` admits only the listed " +
					"addresses (the device UI calls this White List), `false` rejects them (Black List).",
			},
			"access_control_rules": {
				Type:     schema.TypeSet,
				Optional: true,
				Computed: true,
				// Addresses are hashed case-insensitively so that the canonical
				// upper-case form stored in state does not fight a lower-case
				// configuration.
				Set: func(v interface{}) int {
					return schema.HashString(strings.ToUpper(v.(string)))
				},
				Elem: &schema.Schema{
					Type:         schema.TypeString,
					ValidateFunc: validation.StringMatch(macAddressRegexp, "must be a MAC address such as 00:11:22:33:44:55"),
				},
				// The device does not preserve the order of the list it is
				// given (verified on VER-01.06.05-EA), so the registered
				// addresses are modelled as a set. Rule names are device-side
				// and are preserved on write rather than exposed here.
				Description: "MAC addresses registered in this SSID's access control list.",
			},
			"client_limit": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				// The device reports the ceiling in numClient.max (read-only) and
				// the configured limit in numClient.set, which is what this maps to.
				Description: "Maximum number of clients allowed on this SSID (the device's `numClient.set`).",
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
	// NOTE: the "only 0/1 is manageable" restriction deliberately lives in the
	// config validation and in importWifiSSID, not here. Read/Delete also parse
	// the id, so a slot managed before the restriction (e.g. 5g/2) can still be
	// refreshed and removed from state once its block is deleted from the
	// configuration (a config that still declares index >= 2 fails validation,
	// as intended).
	return band, index, nil
}

// importWifiSSID refuses new imports of slots the device web UI cannot operate.
// A slot imported before this restriction can still be read and deleted through
// the permissive parseWifiSSIDID.
func importWifiSSID(_ context.Context, d *schema.ResourceData, _ interface{}) ([]*schema.ResourceData, error) {
	_, index, err := parseWifiSSIDID(d.Id())
	if err != nil {
		return nil, err
	}
	if index != 0 && index != 1 {
		return nil, fmt.Errorf("SSID index %d is not manageable: only the primary (0) and secondary (1) slots are exposed by the device web UI", index)
	}
	return []*schema.ResourceData{d}, nil
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

	// Record the resource before the first write. Writing the access control
	// list as well as the SSID means a failure part-way through would
	// otherwise leave the device changed but the resource untracked, because
	// an empty id is discarded.
	d.SetId(fmt.Sprintf("%s/%d", wifiBandName(band), index))

	raw := d.GetRawConfig()

	// Whether filtering is on right now. The loop below overwrites
	// current.AccessControl with the configured value, so the device's own
	// state is captured first.
	filteringWasOn := current.AccessControl

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
	if set, _ := attrConfigured(raw, "client_limit"); set {
		current.NumClient.Set = d.Get("client_limit").(int)
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

	// The registered MAC addresses live behind their own endpoint. Which side
	// of the SSID write they go on depends on the direction filtering is
	// moving: turning it on is written last, so the list it enforces is
	// already registered, while turning it off is written first, so a run that
	// removes its own address from the list keeps its access.
	password := d.Get("password").(string)
	disableFirst := false
	if set, _ := attrConfigured(raw, "access_control"); set && !d.Get("access_control").(bool) && filteringWasOn {
		if err := client.UpdateWifiSSID(ctx, band, index, *current, password); err != nil {
			return diag.FromErr(err)
		}
		disableFirst = true
	}

	allowSet, _ := attrConfigured(raw, "access_control_allow")
	rulesSet, _ := attrConfigured(raw, "access_control_rules")
	if allowSet || rulesSet {
		// Read first: the only faithful way to write this endpoint's rules is
		// to replay the rules the device already holds, ids included.
		ac, err := client.GetWifiAccessControl(ctx, band, index)
		if err != nil {
			return diag.FromErr(err)
		}
		if ac == nil {
			return diag.Errorf("access control list of SSID %s/%d does not exist on the device", wifiBandName(band), index)
		}
		if rulesSet {
			if err := reconcileAccessControlRules(ctx, client, band, index, ac, d.Get("access_control_rules").(*schema.Set)); err != nil {
				return diag.FromErr(err)
			}
		}
		if allowSet {
			if rulesSet {
				// The reconcile has just changed the list, and the write below
				// replaces it, so replay it as it is now.
				if ac, err = client.GetWifiAccessControl(ctx, band, index); err != nil {
					return diag.FromErr(err)
				}
				if ac == nil {
					return diag.Errorf("access control list of SSID %s/%d does not exist on the device", wifiBandName(band), index)
				}
			}
			ac.Allow = d.Get("access_control_allow").(bool)
			if err := client.UpdateWifiAccessControl(ctx, band, index, *ac); err != nil {
				return diag.FromErr(err)
			}
		}
	}

	if !disableFirst {
		if err := client.UpdateWifiSSID(ctx, band, index, *current, password); err != nil {
			return diag.FromErr(err)
		}
	}

	return resourceWifiSSIDRead(ctx, d, meta)
}

// accessControlRuleName is the placeholder the device itself puts on an address
// it has not seen before. Rule names are not managed here.
const accessControlRuleName = "Unknown Device"

// reconcileAccessControlRules brings an SSID's access control list in line with
// the configured addresses.
//
// Addresses are added and removed one at a time — the way the device's own web
// UI edits the list — because a write carrying the whole list does not replace
// it faithfully unless every rule keeps the id the device assigned it
// (verified on VER-01.06.05-EA).
func reconcileAccessControlRules(ctx context.Context, client *synclayer.Client, band, index int, current *synclayer.WifiAccessControl, configured *schema.Set) error {
	wanted := make(map[string]bool, configured.Len())
	for _, v := range configured.List() {
		wanted[strings.ToUpper(v.(string))] = true
	}

	// Count what the device holds, so duplicates of an address that is kept
	// are recognised as extras.
	present := make(map[string]int, len(current.Rules))
	for _, rule := range current.Rules {
		present[strings.ToUpper(rule.MacAddress)]++
	}

	extra := make([]synclayer.WifiAccessControlRule, 0)
	for _, rule := range current.Rules {
		mac := strings.ToUpper(rule.MacAddress)
		switch {
		case !wanted[mac]:
			extra = append(extra, rule)
		case present[mac] > 1:
			// A duplicate of an address that is kept.
			present[mac]--
			extra = append(extra, rule)
		}
	}

	missing := make([]string, 0, len(wanted))
	for mac := range wanted {
		if present[mac] == 0 {
			missing = append(missing, mac)
		}
	}
	sort.Strings(missing)

	// The order matters while filtering is on: register the new addresses
	// first, so a client whose address is being replaced keeps its access
	// until the replacement is in place. With filtering off there is nothing
	// to lose by removing first, which also keeps a run at the device's rule
	// cap from failing on the temporary overlap.
	if current.Active {
		for _, mac := range missing {
			rule := synclayer.WifiAccessControlRule{Name: accessControlRuleName, MacAddress: mac}
			if err := client.CreateWifiAccessControlRule(ctx, band, index, rule); err != nil {
				return err
			}
		}
		for _, rule := range extra {
			if err := client.DeleteWifiAccessControlRule(ctx, band, index, rule.ID); err != nil {
				return err
			}
		}
		return nil
	}

	for _, rule := range extra {
		if err := client.DeleteWifiAccessControlRule(ctx, band, index, rule.ID); err != nil {
			return err
		}
	}
	for _, mac := range missing {
		rule := synclayer.WifiAccessControlRule{Name: accessControlRuleName, MacAddress: mac}
		if err := client.CreateWifiAccessControlRule(ctx, band, index, rule); err != nil {
			return err
		}
	}
	return nil
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
	// The access control list is a separate endpoint from the SSID itself.
	if ac, err := client.GetWifiAccessControl(ctx, band, index); err != nil {
		return diag.FromErr(err)
	} else if ac != nil {
		if err := d.Set("access_control_allow", ac.Allow); err != nil {
			return diag.FromErr(err)
		}
		macs := make([]string, 0, len(ac.Rules))
		for _, r := range ac.Rules {
			macs = append(macs, strings.ToUpper(r.MacAddress))
		}
		if err := d.Set("access_control_rules", macs); err != nil {
			return diag.FromErr(err)
		}
	}
	if err := d.Set("client_limit", ssid.NumClient.Set); err != nil {
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
