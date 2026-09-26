package synclayer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDevice is a minimal in-memory stand-in for the SXEP200W API.
type fakeDevice struct {
	mu         sync.Mutex
	webKey     string
	validToken string
	tokenSeq   int
	rules      []PortForwardingRule
	nextID     int
	rejectNext bool

	// DDNS support
	rejectNextDDNS bool
	ddnsPasswords  []string // plaintext recovered from received PUT bodies
	ddnsStored     string   // envelope stored by the device (returned on GET)
	ddnsActive     bool
	ddnsProvider   int
	ddnsUsername   string
	ddnsHostname   string
	ddnsURL        string

	// WiFi support
	wifiName        string
	wifiPassword    string
	wifiRadioActive bool

	loginCount int
	expectUser string
	expectPass string
}

func newFakeDevice(user, pass string) *fakeDevice {
	return &fakeDevice{
		webKey:          "test-web-key",
		nextID:          1,
		expectUser:      user,
		expectPass:      pass,
		wifiName:        "test-ssid",
		wifiRadioActive: true,
	}
}

func (f *fakeDevice) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/gateway/users/login/auth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"web_key": f.webKey})
	})
	mux.HandleFunc("/api/v1/gateway/users/login", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			UserName string `json:"userName"`
			Password string `json:"password"`
		}
		_ = json.Unmarshal(body, &req)

		f.mu.Lock()
		defer f.mu.Unlock()
		f.loginCount++
		if req.UserName != f.expectUser || req.Password != EncodeLoginPassword(f.expectUser, f.expectPass, f.webKey) {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]any{"error": map[string]any{"code": 2001, "type": "auth", "message": "bad credentials"}})
			return
		}
		f.tokenSeq++
		// The AES key is derived from the first 16 characters of the token, so
		// the varying part must be at the front for re-authentication to change
		// the key.
		f.validToken = fmt.Sprintf("%016d-access-token", f.tokenSeq)
		writeJSON(w, map[string]any{"accessToken": f.validToken, "expiresIn": 1200})
	})
	mux.HandleFunc("/api/v1/gateway/about", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		writeJSON(w, map[string]any{
			"modelName": "SXEP200W",
			"serialNo":  "TEST0001",
			"hardware":  map[string]string{"version": "HW0.0"},
			"software":  map[string]string{"version": "VER-00.00.00-TEST", "buildTime": "000101_0000"},
			"baseMac":   "02:00:00:00:00:01",
		})
	})
	mux.HandleFunc("/api/v1/service/portForwarding", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			f.mu.Lock()
			defer f.mu.Unlock()
			writeJSON(w, map[string]any{"rules": f.rules, "active": true, "maxRules": 32})
		case http.MethodPost:
			var rule PortForwardingRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			rule.ID = f.nextID
			f.nextID++
			f.rules = append(f.rules, rule)
			f.mu.Unlock()
			writeJSON(w, map[string]int{"id": rule.ID})
		}
	})
	mux.HandleFunc("/api/v1/service/portForwarding/", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/v1/service/portForwarding/"))
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			var rule PortForwardingRule
			_ = json.NewDecoder(r.Body).Decode(&rule)
			for i := range f.rules {
				if f.rules[i].ID == id {
					rule.ID = id
					f.rules[i] = rule
				}
			}
			writeJSON(w, map[string]any{})
		case http.MethodPost:
			for i := range f.rules {
				if f.rules[i].ID == id {
					f.rules = append(f.rules[:i], f.rules[i+1:]...)
					break
				}
			}
			writeJSON(w, map[string]any{})
		}
	})
	mux.HandleFunc("/api/v1/service/ddns", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			f.mu.Lock()
			defer f.mu.Unlock()
			writeJSON(w, map[string]any{
				"active": f.ddnsActive,
				"result": map[string]any{"connectionStatus": "", "returnCode": 0, "ipAddress": "-"},
				"supportingProvider": []map[string]any{
					{"name": "DynDNS", "url": "members.dyndns.org", "domainName": []string{"dyndns.org"}},
					{"name": "NoIP", "url": "dynupdate.no-ip.com", "domainName": []string{"ddns.net"}},
					{"name": "User Define", "url": "", "domainName": []string{""}},
				},
				"configuration": map[string]any{
					"currentProvider": f.ddnsProvider, "username": f.ddnsUsername,
					"password": f.ddnsStored, "token": "",
					"hostname": f.ddnsHostname, "url": f.ddnsURL,
				},
			})
		case http.MethodPut:
			var body struct {
				Active        bool `json:"active"`
				Configuration struct {
					CurrentProvider int    `json:"currentProvider"`
					Username        string `json:"username"`
					Password        string `json:"password"`
					Hostname        string `json:"hostname"`
					URL             string `json:"url"`
				} `json:"configuration"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// A non-empty password must be decryptable with the token presented
			// in the same request.
			plain := ""
			if body.Configuration.Password != "" {
				var err error
				plain, err = DecodeDDNSPassword(r.Header.Get("Access-Token"), body.Configuration.Password)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}

			f.mu.Lock()
			defer f.mu.Unlock()
			if f.rejectNextDDNS {
				f.rejectNextDDNS = false
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, map[string]any{"error": map[string]any{"code": 2003, "type": "invalid_token", "message": "Invalid token"}})
				return
			}
			f.ddnsPasswords = append(f.ddnsPasswords, plain)
			f.ddnsStored = body.Configuration.Password
			f.ddnsActive = body.Active
			f.ddnsProvider = body.Configuration.CurrentProvider
			f.ddnsUsername = body.Configuration.Username
			f.ddnsHostname = body.Configuration.Hostname
			f.ddnsURL = body.Configuration.URL

			writeJSON(w, map[string]any{"connectionStatus": "ok", "returnCode": 0, "ipAddress": "203.0.113.1"})
		}
	})
	mux.HandleFunc("/api/v1/wifi/0/ssid/0", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			// The device returns the stored PSK as a token-keyed envelope, not
			// plaintext (mirrors the real firmware).
			password, err := EncodeDDNSPassword(r.Header.Get("Access-Token"), "admin", f.wifiPassword)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{
				"index": 0, "active": false, "type": "primary", "name": f.wifiName,
				"macAddress": "02:00:00:00:00:10", "accessControl": false, "hiddenSSID": false,
				"APIsolate": false, "webUIAccess": true, "internetOnly": false, "wmf": true, "ft": false,
				"numClient": map[string]any{"max": 75, "set": 75},
				"security": map[string]any{
					"type":     "WPA2-PSK",
					"personal": map[string]any{"password": password, "encryption": "AES", "groupKey": 1800},
					"mfp":      "capable",
				},
			})
		case http.MethodPut:
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var body WifiSSID
			if err := json.Unmarshal(raw, &body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The typed decode cannot tell "key absent" from "empty", so check the
			// raw object too.
			var envelope struct {
				Security struct {
					Personal map[string]any `json:"personal"`
				} `json:"security"`
			}
			_ = json.Unmarshal(raw, &envelope)

			f.wifiName = body.Name
			if pw, ok := envelope.Security.Personal["password"].(string); ok && pw != "" {
				plain, err := DecodeDDNSPassword(r.Header.Get("Access-Token"), pw)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				f.wifiPassword = plain
			} else {
				// An absent or empty password field replaces the PSK on the real
				// device (verified on VER-01.06.05-EA). Mirror that here so a
				// regression to omitting the field fails the preserve test.
				f.wifiPassword = ""
			}
			writeJSON(w, map[string]any{})
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
			writeJSON(w, map[string]any{
				"active": f.wifiRadioActive,
				"basic": map[string]any{
					"wirelessMode": "802.11b+g+n+ax",
					"channel":      map[string]any{"set": "auto", "used": 1},
					"outputPower":  "high",
					"bandwidth":    map[string]any{"set": "40", "used": "20"},
					"sideband":     "upper",
				},
				"advanced": map[string]any{},
				"wmm":      map[string]any{"active": true},
			})
		case http.MethodPut:
			var body WifiRadio
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.wifiRadioActive = body.Active
			writeJSON(w, map[string]any{})
		}
	})
	return mux
}

func (f *fakeDevice) auth(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectNext {
		f.rejectNext = false
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]any{"error": map[string]any{"code": 2003, "type": "invalid_token", "message": "Invalid token"}})
		return false
	}
	if r.Header.Get("Access-Token") != f.validToken || f.validToken == "" {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]any{"error": map[string]any{"code": 2003, "type": "invalid_token", "message": "Invalid token"}})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestClientLoginAndAbout(t *testing.T) {
	dev := newFakeDevice("admin", "s3cret")
	srv := httptest.NewServer(dev.handler())
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}

	about, err := client.About(ctx)
	if err != nil {
		t.Fatalf("About: %v", err)
	}
	if about.ModelName != "SXEP200W" || about.SerialNo != "TEST0001" {
		t.Fatalf("unexpected about: %+v", about)
	}
}

func TestClientPortForwardingLifecycle(t *testing.T) {
	dev := newFakeDevice("admin", "s3cret")
	srv := httptest.NewServer(dev.handler())
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	rule := PortForwardingRule{
		Active:       true,
		ServiceType:  "SSH",
		IPAddress:    "192.168.0.10",
		Protocol:     "tcp",
		LocalPort:    PortRange{Start: 22, End: 22},
		ExternalPort: PortRange{Start: 2222, End: 2222},
	}

	id, err := client.CreatePortForwarding(ctx, rule)
	if err != nil {
		t.Fatalf("CreatePortForwarding: %v", err)
	}

	rules, err := client.ListPortForwarding(ctx)
	if err != nil {
		t.Fatalf("ListPortForwarding: %v", err)
	}
	if len(rules) != 1 || rules[0].ServiceType != "SSH" {
		t.Fatalf("unexpected rules: %+v", rules)
	}

	rule.ServiceType = "SSH-alt"
	if err := client.UpdatePortForwarding(ctx, id, rule); err != nil {
		t.Fatalf("UpdatePortForwarding: %v", err)
	}
	got, err := client.GetPortForwarding(ctx, id)
	if err != nil {
		t.Fatalf("GetPortForwarding: %v", err)
	}
	if got == nil || got.ServiceType != "SSH-alt" {
		t.Fatalf("update not applied: %+v", got)
	}

	if err := client.DeletePortForwarding(ctx, id); err != nil {
		t.Fatalf("DeletePortForwarding: %v", err)
	}
	got, err = client.GetPortForwarding(ctx, id)
	if err != nil {
		t.Fatalf("GetPortForwarding after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("rule still present after delete: %+v", got)
	}
}

func TestClientReauthenticatesOnUnauthorized(t *testing.T) {
	dev := newFakeDevice("admin", "s3cret")
	srv := httptest.NewServer(dev.handler())
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}

	dev.mu.Lock()
	dev.rejectNext = true
	dev.mu.Unlock()

	// The first About call is answered with 401; the client must transparently
	// re-authenticate and retry.
	if _, err := client.About(ctx); err != nil {
		t.Fatalf("About after forced 401: %v", err)
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()
	if dev.loginCount != 2 {
		t.Fatalf("expected 2 logins (initial + re-auth), got %d", dev.loginCount)
	}
}

// TestUpdateDDNSReencryptsAfterReauth ensures that when the DDNS PUT is
// retried after a transparent re-authentication, the password is re-encrypted
// with the new token rather than replaying ciphertext bound to the old token.
func TestUpdateDDNSReencryptsAfterReauth(t *testing.T) {
	dev := newFakeDevice("admin", "s3cret")
	srv := httptest.NewServer(dev.handler())
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}

	dev.mu.Lock()
	dev.rejectNextDDNS = true
	dev.mu.Unlock()

	if _, err := client.UpdateDDNS(ctx, true, DDNSConfiguration{
		CurrentProvider: 0,
		Username:        "ddnsuser",
		Password:        "top-secret",
		Hostname:        "example.dyndns.org",
	}); err != nil {
		t.Fatalf("UpdateDDNS: %v", err)
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()
	if dev.loginCount != 2 {
		t.Fatalf("expected 2 logins (initial + re-auth), got %d", dev.loginCount)
	}
	if len(dev.ddnsPasswords) != 1 {
		t.Fatalf("expected exactly 1 accepted DDNS request, got %d", len(dev.ddnsPasswords))
	}
	if dev.ddnsPasswords[0] != "top-secret" {
		t.Fatalf("DDNS password was not decodable with the token in use: got %q", dev.ddnsPasswords[0])
	}
}

// TestAPIErrorMessagePreserved ensures that actionable response details survive
// even when the device does not send a structured error code.
func TestAPIErrorMessagePreserved(t *testing.T) {
	if err := parseAPIError(400, []byte(`{"error":{"message":"rule limit exceeded"}}`)); !strings.Contains(err.Error(), "rule limit exceeded") {
		t.Fatalf("structured message lost: %v", err)
	}
	if err := parseAPIError(500, []byte("boom")); !strings.Contains(err.Error(), "boom") {
		t.Fatalf("plain-text message lost: %v", err)
	}
}

// TestInvalidateTokenOnlyClearsMatching verifies that a stale 401 response can
// not discard a token that a concurrent operation has already refreshed.
func TestInvalidateTokenOnlyClearsMatching(t *testing.T) {
	c := &Client{token: "fresh", expiry: time.Now().Add(time.Hour)}

	if c.invalidateToken("stale") {
		t.Fatal("invalidateToken must not clear a token that was already refreshed")
	}
	if c.token != "fresh" {
		t.Fatalf("fresh token was lost: %q", c.token)
	}

	if !c.invalidateToken("fresh") {
		t.Fatal("invalidateToken should clear the matching token")
	}
	if c.token != "" {
		t.Fatalf("token was not cleared: %q", c.token)
	}
}

// TestClientRefusesCrossOriginRedirect ensures the device session token is not
// forwarded when an endpoint redirects to a different origin.
func TestClientRefusesCrossOriginRedirect(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Access-Token") != "" {
			leaked.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/gateway/users/login/auth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"web_key": "redirect-test-key"})
	})
	mux.HandleFunc("/api/v1/gateway/users/login", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"accessToken": "test-access-token-000000000000", "expiresIn": 1200})
	})
	mux.HandleFunc("/api/v1/gateway/about", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	})
	device := httptest.NewServer(mux)
	defer device.Close()

	client, err := NewClient(Config{BaseURL: device.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := client.About(ctx); err == nil {
		t.Fatal("expected a cross-origin redirect to be refused")
	}
	if leaked.Load() {
		t.Fatal("Access-Token was forwarded to a different origin")
	}
}

// TestUpdateDDNSKeepsStoredPassword ensures an update that does not supply a
// password (for example after terraform import) does not wipe the credential
// the device already stores.
func TestUpdateDDNSKeepsStoredPassword(t *testing.T) {
	dev := newFakeDevice("admin", "s3cret")
	srv := httptest.NewServer(dev.handler())
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	if _, err := client.UpdateDDNS(ctx, true, DDNSConfiguration{
		CurrentProvider: 0, Username: "ddnsuser", Password: "top-secret", Hostname: "a.example.com",
	}); err != nil {
		t.Fatalf("initial UpdateDDNS: %v", err)
	}

	// Second update without a password must recover and re-send the stored one.
	if _, err := client.UpdateDDNS(ctx, true, DDNSConfiguration{
		CurrentProvider: 0, Username: "ddnsuser", Hostname: "b.example.com",
	}); err != nil {
		t.Fatalf("update without password: %v", err)
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()
	if len(dev.ddnsPasswords) != 2 {
		t.Fatalf("expected 2 accepted DDNS requests, got %d", len(dev.ddnsPasswords))
	}
	for i, got := range dev.ddnsPasswords {
		if got != "top-secret" {
			t.Fatalf("request %d password = %q, want %q (credential was not preserved)", i, got, "top-secret")
		}
	}
	if dev.ddnsStored == "" {
		t.Fatal("stored password was cleared")
	}
	if dev.ddnsHostname != "b.example.com" {
		t.Fatalf("hostname not updated: %q", dev.ddnsHostname)
	}
}

// TestDisableDDNSRetainsConfiguration ensures destroy only disables the client
// and keeps the stored account configuration.
func TestDisableDDNSRetainsConfiguration(t *testing.T) {
	dev := newFakeDevice("admin", "s3cret")
	srv := httptest.NewServer(dev.handler())
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	if _, err := client.UpdateDDNS(ctx, true, DDNSConfiguration{
		CurrentProvider: 2, Username: "ddnsuser", Password: "top-secret",
		Hostname: "a.example.com", URL: "updates.example.com",
	}); err != nil {
		t.Fatalf("initial UpdateDDNS: %v", err)
	}

	if err := client.DisableDDNS(ctx); err != nil {
		t.Fatalf("DisableDDNS: %v", err)
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()
	if dev.ddnsActive {
		t.Fatal("DDNS is still active")
	}
	if dev.ddnsStored == "" {
		t.Fatal("DisableDDNS cleared the stored password")
	}
	if dev.ddnsUsername != "ddnsuser" || dev.ddnsHostname != "a.example.com" || dev.ddnsURL != "updates.example.com" || dev.ddnsProvider != 2 {
		t.Fatalf("DisableDDNS cleared configuration: %+v", dev)
	}
}

// TestClientWifi covers the SSID and radio endpoints, including that the SSID
// password is obfuscated with the session token before it is sent.
func TestClientWifi(t *testing.T) {
	dev := newFakeDevice("admin", "s3cret")
	srv := httptest.NewServer(dev.handler())
	defer srv.Close()

	client, err := NewClient(Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	ssid, err := client.GetWifiSSID(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetWifiSSID: %v", err)
	}
	if ssid == nil || ssid.Name != "test-ssid" {
		t.Fatalf("unexpected SSID: %+v", ssid)
	}

	ssid.Name = "renamed"
	if err := client.UpdateWifiSSID(ctx, 0, 0, *ssid, "top-secret"); err != nil {
		t.Fatalf("UpdateWifiSSID: %v", err)
	}

	dev.mu.Lock()
	if dev.wifiName != "renamed" {
		t.Errorf("SSID name not applied: %q", dev.wifiName)
	}
	if dev.wifiPassword != "top-secret" {
		t.Errorf("SSID password was not encoded with the session token: %q", dev.wifiPassword)
	}
	dev.mu.Unlock()

	// A 401 on the read must recover the PSK with the token that actually
	// produced the envelope (the retried one), not the first attempt's.
	dev.mu.Lock()
	dev.rejectNext = true
	dev.mu.Unlock()
	retried, err := client.GetWifiSSID(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetWifiSSID (after 401): %v", err)
	}
	if retried.Security.Personal == nil || !retried.Security.Personal.PasswordRecovered {
		t.Errorf("PSK was not recovered after a GET retry")
	}

	// An update without a password must keep the stored one: the device returns
	// it as a token-keyed envelope, which the client recovers and re-encodes.
	preserved, err := client.GetWifiSSID(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetWifiSSID (preserve): %v", err)
	}
	preserved.Name = "renamed-again"
	if err := client.UpdateWifiSSID(ctx, 0, 0, *preserved, ""); err != nil {
		t.Fatalf("UpdateWifiSSID (preserve): %v", err)
	}
	dev.mu.Lock()
	if dev.wifiName != "renamed-again" {
		t.Errorf("SSID name not applied on preserve update: %q", dev.wifiName)
	}
	if dev.wifiPassword != "top-secret" {
		t.Errorf("stored password not preserved: %q", dev.wifiPassword)
	}
	dev.mu.Unlock()

	// A re-authentication between the read and the write must not corrupt the
	// PSK: GetWifiSSID recovers the password with the token that produced the
	// envelope, and the update re-encodes it with the token in use.
	reauthed, err := client.GetWifiSSID(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetWifiSSID (re-auth): %v", err)
	}
	dev.mu.Lock()
	dev.rejectNext = true // force one 401 so the client re-authenticates
	dev.mu.Unlock()

	reauthed.Name = "renamed-after-auth"
	if err := client.UpdateWifiSSID(ctx, 0, 0, *reauthed, ""); err != nil {
		t.Fatalf("UpdateWifiSSID (re-auth): %v", err)
	}
	dev.mu.Lock()
	if dev.wifiName != "renamed-after-auth" {
		t.Errorf("SSID name not applied across re-auth: %q", dev.wifiName)
	}
	if dev.wifiPassword != "top-secret" {
		t.Errorf("stored password corrupted across re-auth: %q", dev.wifiPassword)
	}
	dev.mu.Unlock()

	radio, err := client.GetWifiRadio(ctx, 0)
	if err != nil {
		t.Fatalf("GetWifiRadio: %v", err)
	}
	if !radio.Active {
		t.Fatal("expected radio to be active")
	}
	radio.Active = false
	if err := client.UpdateWifiRadio(ctx, 0, *radio); err != nil {
		t.Fatalf("UpdateWifiRadio: %v", err)
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()
	if dev.wifiRadioActive {
		t.Error("radio active flag not applied")
	}
}
