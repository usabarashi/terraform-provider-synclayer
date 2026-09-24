package synclayer

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
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
	ddnsExpectUser string

	loginCount int
	expectUser string
	expectPass string
}

func newFakeDevice(user, pass string) *fakeDevice {
	return &fakeDevice{
		webKey:     "test-web-key",
		nextID:     1,
		expectUser: user,
		expectPass: pass,
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
		// The token must be long enough to derive a 16 byte AES key.
		f.validToken = fmt.Sprintf("test-access-token-%016d", f.tokenSeq)
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
			writeJSON(w, map[string]any{
				"active": false,
				"result": map[string]any{"connectionStatus": "", "returnCode": 0, "ipAddress": "-"},
				"supportingProvider": []map[string]any{
					{"name": "DynDNS", "url": "members.dyndns.org", "domainName": []string{"dyndns.org"}},
					{"name": "NoIP", "url": "dynupdate.no-ip.com", "domainName": []string{"ddns.net"}},
					{"name": "User Define", "url": "", "domainName": []string{""}},
				},
				"configuration": map[string]any{
					"currentProvider": 0, "username": "", "password": "", "token": "",
					"hostname": "", "url": "members.dyndns.org",
				},
			})
		case http.MethodPut:
			var body struct {
				Configuration struct {
					Username string `json:"username"`
					Password string `json:"password"`
				} `json:"configuration"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// The password must be decryptable with the token presented in the
			// same request.
			plain, err := decodeDDNSPassword(r.Header.Get("Access-Token"), body.Configuration.Username, body.Configuration.Password)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			f.mu.Lock()
			if f.rejectNextDDNS {
				f.rejectNextDDNS = false
				f.mu.Unlock()
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, map[string]any{"error": map[string]any{"code": 2003, "type": "invalid_token", "message": "Invalid token"}})
				return
			}
			f.ddnsPasswords = append(f.ddnsPasswords, plain)
			f.mu.Unlock()

			writeJSON(w, map[string]any{"connectionStatus": "ok", "returnCode": 0, "ipAddress": "203.0.113.1"})
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

// decodeDDNSPassword is a test-only inverse of EncodeDDNSPassword. It mimics
// what the firmware does: derive the AES key from the request's access token
// and unwrap "HS\x0e<user>\x0e<hex>" after base64 decoding.
func decodeDDNSPassword(accessToken, userName, encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	prefix := "HS\x0e" + userName + "\x0e"
	if !strings.HasPrefix(string(raw), prefix) {
		return "", fmt.Errorf("missing password envelope")
	}
	ciphertext, err := hex.DecodeString(string(raw)[len(prefix):])
	if err != nil {
		return "", fmt.Errorf("hex decode: %w", err)
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length %d", len(ciphertext))
	}

	key := accessToken
	if len(key) > 16 {
		key = key[:16]
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, []byte("0123456789012345")).CryptBlocks(plaintext, ciphertext)

	return string(bytes.TrimRight(plaintext, "\x00")), nil
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
