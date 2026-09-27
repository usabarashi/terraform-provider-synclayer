package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

// wifiFake is a minimal SXEP200W stand-in for the WiFi SSID endpoints: the login
// handshake, GET/PUT of one SSID, and the token-keyed PSK envelope the device
// uses (verified on VER-01.06.05-EA).
type wifiFake struct {
	mu          sync.Mutex
	webKey      string
	token       string
	tokenSeq    int
	name        string
	password    string
	hiddenSSID  bool
	clientLimit int
	active      bool

	radioActive    bool
	radioChannel   string
	radioBandwidth string

	acActive bool
	acAllow  bool
	acRules  []synclayer.WifiAccessControlRule

	wpsActive bool
	wpsPIN    string

	meshMode int
	meshBand string

	// failSSIDWrite and failMeshWrite make the respective endpoint reject
	// writes, so tests can check what a half-finished write leaves behind.
	failSSIDWrite bool
	failMeshWrite bool

	// events records the writes the fake accepted, in order, so tests can
	// assert on how they were sequenced.
	events []string
}

func newWifiFake() *wifiFake {
	return &wifiFake{
		webKey:         "test-web-key",
		name:           "initial",
		password:       "stored-psk",
		clientLimit:    75,
		active:         true,
		radioActive:    true,
		radioChannel:   "auto",
		radioBandwidth: "40",
		acAllow:        true,
		wpsActive:      true,
		wpsPIN:         "00000000",
	}
}

func (f *wifiFake) state() (name, password string, hidden, active bool, clientLimit int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name, f.password, f.hiddenSSID, f.active, f.clientLimit
}

func (f *wifiFake) auth(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token == "" || r.Header.Get("Access-Token") != f.token {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":2003,"type":"invalid_token","message":"Invalid token"}}`))
		return false
	}
	return true
}

func (f *wifiFake) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/gateway/users/login/auth", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"web_key": f.webKey})
	})

	mux.HandleFunc("/api/v1/gateway/users/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UserName string `json:"userName"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		f.mu.Lock()
		defer f.mu.Unlock()
		if req.UserName != "admin" || req.Password != synclayer.EncodeLoginPassword("admin", "s3cret", f.webKey) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.tokenSeq++
		f.token = fmt.Sprintf("%016d-access-token", f.tokenSeq)
		_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": f.token, "expiresIn": 1200})
	})

	// One SSID slot is served at 2.4g/0; the extra slot the restriction is about
	// (5g/2) is served too, so the cleanup path can be exercised.
	ssidHandler := func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			password, err := synclayer.EncodeDDNSPassword(f.token, "admin", f.password)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"index": 0, "active": f.active, "type": "primary", "name": f.name,
				"macAddress": "02:00:00:00:00:10", "accessControl": f.acActive, "hiddenSSID": f.hiddenSSID,
				"APIsolate": false, "webUIAccess": true, "internetOnly": false, "wmf": true, "ft": false,
				"numClient": map[string]any{"max": 75, "set": f.clientLimit},
				"security": map[string]any{
					"type":     "WPA2-PSK",
					"personal": map[string]any{"password": password, "encryption": "AES", "groupKey": 1800},
					"mfp":      "capable",
				},
			})
		case http.MethodPut:
			if f.failSSIDWrite {
				http.Error(w, `{"error":{"code":1,"type":"internal","message":"simulated failure"}}`, http.StatusInternalServerError)
				return
			}
			var body synclayer.WifiSSID
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.name = body.Name
			f.hiddenSSID = body.HiddenSSID
			f.active = body.Active
			f.acActive = body.AccessControl
			f.events = append(f.events, fmt.Sprintf("ssid_put:accessControl=%v", body.AccessControl))
			if body.NumClient.Set != 0 {
				f.clientLimit = body.NumClient.Set
			}
			if body.Security.Personal != nil && body.Security.Personal.Password != "" {
				plain, err := synclayer.DecodeDDNSPassword(f.token, body.Security.Personal.Password)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				f.password = plain
			} else {
				// An absent/empty password replaces the PSK on the device.
				f.password = ""
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
	mux.HandleFunc("/api/v1/wifi/0/ssid/0", ssidHandler)
	mux.HandleFunc("/api/v1/wifi/1/ssid/2", ssidHandler)

	mux.HandleFunc("/api/v1/wifi/0/radio", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			// The device answers 202 with a body (verified on VER-01.06.05-EA).
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"active": f.radioActive,
				"basic": map[string]any{
					"wirelessMode": "802.11b+g+n+ax",
					"channel":      map[string]any{"set": f.radioChannel, "used": 1},
					"outputPower":  "high",
					"bandwidth":    map[string]any{"set": f.radioBandwidth, "used": "20"},
					"sideband":     "upper",
				},
				"advanced":    map[string]any{"countryCode": "JP"},
				"wmm":         map[string]any{"active": true},
				"channelList": []map[string]any{{"channel": 1}, {"channel": 6}},
			})
		case http.MethodPut:
			var body synclayer.WifiRadio
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.radioActive = body.Active
			f.radioChannel = body.Basic.Channel.Set
			f.radioBandwidth = body.Basic.Bandwidth.Set
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	// MAC access control of one SSID slot. The list is served with the ids the
	// device assigned, and changed one rule at a time, as the device does.
	accessControlList := func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			rules := f.acRules
			// The device does not preserve the order of the rules it is given
			// (verified on VER-01.06.05-EA), so rotate them here: an
			// order-sensitive provider implementation then shows a diff.
			if len(rules) > 1 {
				rotated := make([]synclayer.WifiAccessControlRule, 0, len(rules))
				rotated = append(rotated, rules[1:]...)
				rotated = append(rotated, rules[0])
				rules = rotated
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"active": f.acActive, "allow": f.acAllow, "maxRules": 32, "rules": rules,
			})
		case http.MethodPut:
			// A whole-list write is only faithful when every rule keeps the id
			// the device assigned, so rules are decoded raw and a write that
			// drops an id is refused here the way the device mangles it
			// (verified on VER-01.06.05-EA).
			var body struct {
				Active bool             `json:"active"`
				Allow  bool             `json:"allow"`
				Rules  []map[string]any `json:"rules"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rules := make([]synclayer.WifiAccessControlRule, 0, len(body.Rules))
			for _, raw := range body.Rules {
				id, ok := raw["id"].(float64)
				if !ok {
					http.Error(w, "access control rule sent without an id", http.StatusBadRequest)
					return
				}
				name, _ := raw["name"].(string)
				mac, _ := raw["macAddress"].(string)
				rules = append(rules, synclayer.WifiAccessControlRule{ID: int(id), Name: name, MacAddress: mac})
			}
			f.acRules = rules
			f.acActive = body.Active
			f.acAllow = body.Allow
			f.events = append(f.events, fmt.Sprintf("access_control_put:rules=%d", len(rules)))
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case http.MethodPost:
			var body synclayer.WifiAccessControlRule
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			next := 0
			for _, rule := range f.acRules {
				if rule.ID >= next {
					next = rule.ID + 1
				}
			}
			body.ID = next
			f.acRules = append(f.acRules, body)
			f.events = append(f.events, "rule_create:"+body.MacAddress)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
	accessControlDelete := func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		id, err := strconv.Atoi(path.Base(r.URL.Path))
		if err != nil {
			http.Error(w, "invalid rule id", http.StatusBadRequest)
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()
		kept := make([]synclayer.WifiAccessControlRule, 0, len(f.acRules))
		for _, rule := range f.acRules {
			if rule.ID != id {
				kept = append(kept, rule)
			}
		}
		f.acRules = kept
		f.events = append(f.events, fmt.Sprintf("rule_delete:%d", id))
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}
	for _, base := range []string{"/api/v1/wifi/0/ssid/0/accessControl", "/api/v1/wifi/1/ssid/2/accessControl"} {
		mux.HandleFunc(base, accessControlList)
		mux.HandleFunc(base+"/", accessControlDelete)
	}

	mux.HandleFunc("/api/v1/wifi/wps", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"active": f.wpsActive, "pin": f.wpsPIN, "status": "Idle"})
		case http.MethodPut:
			// Only the active flag is writable: the PIN and the pairing status
			// must never be part of a write.
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			for key := range body {
				if key != "active" {
					http.Error(w, "unexpected field in the WPS write: "+key, http.StatusBadRequest)
					return
				}
			}
			active, _ := body["active"].(bool)
			f.wpsActive = active
			f.events = append(f.events, fmt.Sprintf("wps_put:active=%v", active))
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/wifi/meshmode", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"mode": f.meshMode, "bhBand": f.meshBand,
				"supportBhApBand": []string{"6"}, "supportBhStaBand": []string{"6"},
			})
		case http.MethodPut:
			if f.failMeshWrite {
				http.Error(w, `{"error":{"code":1,"type":"internal","message":"simulated failure"}}`, http.StatusInternalServerError)
				return
			}
			// Only mode and bhBand are writable.
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			for key := range body {
				if key != "mode" && key != "bhBand" {
					http.Error(w, "unexpected field in the mesh write: "+key, http.StatusBadRequest)
					return
				}
			}
			mode, _ := body["mode"].(float64)
			band, _ := body["bhBand"].(string)
			f.meshMode = int(mode)
			f.meshBand = band
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	return mux
}

func TestWifiSSIDPartialUpdate(t *testing.T) {
	f := newWifiFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	checkState := func(name string, hidden, active bool, clientLimit int) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			gotName, gotPassword, gotHidden, gotActive, gotClientLimit := f.state()
			if gotName != name {
				return fmt.Errorf("device name = %q, want %q", gotName, name)
			}
			if gotHidden != hidden {
				return fmt.Errorf("device hidden = %v, want %v", gotHidden, hidden)
			}
			if gotActive != active {
				return fmt.Errorf("device active = %v, want %v", gotActive, active)
			}
			if gotClientLimit != clientLimit {
				return fmt.Errorf("device client limit = %d, want %d", gotClientLimit, clientLimit)
			}
			if gotPassword != "stored-psk" {
				return fmt.Errorf("device PSK = %q, want %q (must be preserved)", gotPassword, "stored-psk")
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			// Destroying the resource disables the SSID and keeps the PSK.
			_, gotPassword, _, gotActive, _ := f.state()
			if gotActive {
				return fmt.Errorf("SSID is still active after destroy")
			}
			if gotPassword != "stored-psk" {
				return fmt.Errorf("PSK changed on destroy: %q", gotPassword)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Only some attributes are set: the rest keep their device value
				// and the PSK is preserved through the update.
				Config: `
resource "synclayer_sxep200w_wifi_ssid" "t" {
  band  = "2.4g"
  index = 0
  name  = "first"
}`,
				Check: checkState("first", false, true, 75),
			},
			{
				Config: `
resource "synclayer_sxep200w_wifi_ssid" "t" {
  band        = "2.4g"
  index       = 0
  name        = "second"
  hidden_ssid = true
}`,
				Check: checkState("second", true, true, 75),
			},
			{
				Config: `
resource "synclayer_sxep200w_wifi_ssid" "t" {
  band         = "2.4g"
  index        = 0
  name         = "second"
  hidden_ssid  = true
  client_limit = 70
}`,
				Check: checkState("second", true, true, 70),
			},
			{
				ResourceName:            "synclayer_sxep200w_wifi_ssid.t",
				ImportState:             true,
				ImportStateId:           "2.4g/0",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				// Slots the web UI cannot operate must not be imported.
				ResourceName:  "synclayer_sxep200w_wifi_ssid.t",
				ImportState:   true,
				ImportStateId: "5g/2",
				ExpectError:   regexp.MustCompile("SSID index 2 is not manageable"),
			},
		},
	})
}

func TestWifiSSIDCleanupPath(t *testing.T) {
	// A slot managed before the restriction (e.g. 5g/2) must still be readable
	// and deletable, so it can be removed from state by deleting its block.
	f := newWifiFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	client, err := synclayer.NewClient(synclayer.Config{
		BaseURL:  srv.URL,
		Username: "admin",
		Password: "s3cret",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}

	r := resourceWifiSSID()
	d := schema.TestResourceDataRaw(t, r.Schema, nil)
	d.SetId("5g/2")

	if diags := resourceWifiSSIDRead(ctx, d, client); diags.HasError() {
		t.Fatalf("read 5g/2: %v", diags)
	}
	if d.Id() == "" {
		t.Fatal("read cleared the id unexpectedly")
	}
	if got := d.Get("name").(string); got != "initial" {
		t.Errorf("read name = %q, want %q", got, "initial")
	}

	if diags := resourceWifiSSIDDelete(ctx, d, client); diags.HasError() {
		t.Fatalf("delete 5g/2: %v", diags)
	}
	if d.Id() != "" {
		t.Errorf("delete did not clear the id")
	}
	if _, password, _, active, _ := f.state(); active {
		t.Errorf("deleting the slot should have disabled it")
	} else if password != "stored-psk" {
		t.Errorf("PSK changed while deleting: %q", password)
	}
}

func TestWifiRadioLifecycle(t *testing.T) {
	f := newWifiFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	checkRadio := func(active bool, bandwidth string) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.radioActive != active {
				return fmt.Errorf("radio active = %v, want %v", f.radioActive, active)
			}
			if f.radioBandwidth != bandwidth {
				return fmt.Errorf("radio bandwidth = %q, want %q", f.radioBandwidth, bandwidth)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			// Destroying the resource turns the radio off.
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.radioActive {
				return fmt.Errorf("radio is still active after destroy")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: `
resource "synclayer_sxep200w_wifi_radio" "t" {
  band         = "2.4g"
  active       = true
  channel      = "auto"
  bandwidth    = "40"
  output_power = "high"
}`,
				Check: checkRadio(true, "40"),
			},
			{
				Config: `
resource "synclayer_sxep200w_wifi_radio" "t" {
  band      = "2.4g"
  active    = true
  bandwidth = "20"
}`,
				Check: checkRadio(true, "20"),
			},
			{
				ResourceName:      "synclayer_sxep200w_wifi_radio.t",
				ImportState:       true,
				ImportStateId:     "2.4g",
				ImportStateVerify: false,
			},
		},
	})
}

// sameStringSet reports whether two string slices hold the same values,
// ignoring order.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[strings.ToUpper(v)]++
	}
	for _, v := range b {
		counts[strings.ToUpper(v)]--
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestWifiSSIDAccessControl(t *testing.T) {
	f := newWifiFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	checkAccessControl := func(active, allow bool, macs ...string) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.acActive != active {
				return fmt.Errorf("device access control active = %v, want %v", f.acActive, active)
			}
			if f.acAllow != allow {
				return fmt.Errorf("device access control allow = %v, want %v", f.acAllow, allow)
			}
			got := make([]string, 0, len(f.acRules))
			for _, r := range f.acRules {
				got = append(got, r.MacAddress)
				if r.Name == "" {
					return fmt.Errorf("rule %s was written without a name", r.MacAddress)
				}
			}
			if !sameStringSet(got, macs) {
				return fmt.Errorf("registered MACs = %v, want %v", got, macs)
			}
			return nil
		}
	}

	// checkOrder pins the ordering the device needs. Filtering must be turned
	// on only after the addresses it enforces are registered (or the operator
	// is locked out mid-apply), and turned off before the list is edited (or a
	// run that removes its own address loses access). Only the events of the
	// step being checked are considered.
	checkOrder := func(enable bool) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()

			events := f.events
			for i := len(events) - 1; i >= 0; i-- {
				if events[i] == "step_start" {
					events = events[i+1:]
					break
				}
			}

			marker := fmt.Sprintf("ssid_put:accessControl=%v", enable)
			at := -1
			for i, e := range events {
				if e == marker {
					at = i
					break
				}
			}
			if at < 0 {
				return fmt.Errorf("%s missing from %v", marker, events)
			}
			for i, e := range events {
				if !strings.HasPrefix(e, "rule_create:") && !strings.HasPrefix(e, "rule_delete:") {
					continue
				}
				if enable && i > at {
					return fmt.Errorf("list changed after filtering was enabled (%s): %v", e, events)
				}
				if !enable && i < at {
					return fmt.Errorf("list changed before filtering was disabled (%s): %v", e, events)
				}
			}
			return nil
		}
	}

	// snapshot marks the start of a step's events.
	snapshot := func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.events = append(f.events, "step_start")
	}

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		Steps: []resource.TestStep{
			{
				PreConfig: snapshot,
				// The list is registered with mixed case on purpose: the
				// canonical upper-case form must not fight the configuration.
				Config: `
resource "synclayer_sxep200w_wifi_ssid" "t" {
  band                 = "2.4g"
  index                = 0
  name                 = "initial"
  access_control       = true
  access_control_allow = true
  access_control_rules = ["aa:bb:cc:dd:ee:01", "AA:BB:CC:DD:EE:02"]
}`,
				Check: resource.ComposeTestCheckFunc(
					checkAccessControl(true, true, "AA:BB:CC:DD:EE:01", "AA:BB:CC:DD:EE:02"),
					checkOrder(true),
				),
			},
			{
				// The same set in another order: the device reorders what it
				// returns, so nothing may be planned here.
				Config: `
resource "synclayer_sxep200w_wifi_ssid" "t" {
  band                 = "2.4g"
  index                = 0
  name                 = "initial"
  access_control       = true
  access_control_allow = true
  access_control_rules = ["AA:BB:CC:DD:EE:02", "AA:BB:CC:DD:EE:01"]
}`,
				Check: checkAccessControl(true, true, "AA:BB:CC:DD:EE:01", "AA:BB:CC:DD:EE:02"),
			},
			{
				// Filtering is turned off and the list is shortened in the same
				// apply, so it must be disabled before the address is removed.
				PreConfig: snapshot,
				Config: `
resource "synclayer_sxep200w_wifi_ssid" "t" {
  band                 = "2.4g"
  index                = 0
  name                 = "initial"
  access_control       = false
  access_control_allow = false
  access_control_rules = ["AA:BB:CC:DD:EE:01"]
}`,
				Check: resource.ComposeTestCheckFunc(
					checkAccessControl(false, false, "AA:BB:CC:DD:EE:01"),
					checkOrder(false),
				),
			},
			{
				ResourceName:            "synclayer_sxep200w_wifi_ssid.t",
				ImportState:             true,
				ImportStateId:           "2.4g/0",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
		},
	})
}

func TestWifiSSIDPartialFailureKeepsID(t *testing.T) {
	// The resource is recorded before its first write, so a write that fails
	// leaves a resource Terraform can still track rather than a change on the
	// device with nothing pointing at it. The direct call is enough here: the
	// configured attributes are not involved, only the id and the SSID write.
	f := newWifiFake()
	f.failSSIDWrite = true
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	client, err := synclayer.NewClient(synclayer.Config{
		BaseURL:  srv.URL,
		Username: "admin",
		Password: "s3cret",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}

	r := resourceWifiSSID()
	d := schema.TestResourceDataRaw(t, r.Schema, map[string]interface{}{
		"band":  "2.4g",
		"index": 0,
		"name":  "initial",
	})

	if diags := resourceWifiSSIDUpsert(ctx, d, client); !diags.HasError() {
		t.Fatal("expected the SSID write to fail")
	}
	if d.Id() != "2.4g/0" {
		t.Errorf("id = %q after a failed write, want %q", d.Id(), "2.4g/0")
	}
}
