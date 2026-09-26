package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	mu         sync.Mutex
	webKey     string
	token      string
	tokenSeq   int
	name       string
	password   string
	hiddenSSID bool

	radioActive    bool
	radioChannel   string
	radioBandwidth string
}

func newWifiFake() *wifiFake {
	return &wifiFake{
		webKey:         "test-web-key",
		name:           "initial",
		password:       "stored-psk",
		radioActive:    true,
		radioChannel:   "auto",
		radioBandwidth: "40",
	}
}

func (f *wifiFake) state() (name, password string, hidden bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name, f.password, f.hiddenSSID
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

	mux.HandleFunc("/api/v1/wifi/0/ssid/0", func(w http.ResponseWriter, r *http.Request) {
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
				"index": 0, "active": true, "type": "primary", "name": f.name,
				"macAddress": "02:00:00:00:00:10", "accessControl": false, "hiddenSSID": f.hiddenSSID,
				"APIsolate": false, "webUIAccess": true, "internetOnly": false, "wmf": true, "ft": false,
				"numClient": map[string]any{"max": 75, "set": 75},
				"security": map[string]any{
					"type":     "WPA2-PSK",
					"personal": map[string]any{"password": password, "encryption": "AES", "groupKey": 1800},
					"mfp":      "capable",
				},
			})
		case http.MethodPut:
			var body synclayer.WifiSSID
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.name = body.Name
			f.hiddenSSID = body.HiddenSSID
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
	})

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

	return mux
}

func TestWifiSSIDPartialUpdate(t *testing.T) {
	f := newWifiFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	checkState := func(name string, hidden bool) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			gotName, gotPassword, gotHidden := f.state()
			if gotName != name {
				return fmt.Errorf("device name = %q, want %q", gotName, name)
			}
			if gotHidden != hidden {
				return fmt.Errorf("device hidden = %v, want %v", gotHidden, hidden)
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
				Check: checkState("first", false),
			},
			{
				Config: `
resource "synclayer_sxep200w_wifi_ssid" "t" {
  band        = "2.4g"
  index       = 0
  name        = "second"
  hidden_ssid = true
}`,
				Check: checkState("second", true),
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
