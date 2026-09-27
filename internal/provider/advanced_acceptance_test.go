package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"

	"github.com/usabarashi/terraform-provider-synclayer/internal/synclayer"
)

// advancedFake is a stand-in for the endpoints behind the web UI's Advanced
// menu. It reproduces the parts of the device's behaviour the provider has to
// work around, all verified on VER-01.06.05-EA:
//
//   - a partial write of the network options is refused,
//   - a write of the ALG list replaces the whole list,
//   - a write of UPnP must carry all three fields,
//   - an IPv6 route update re-creates the route under a new id.
type advancedFake struct {
	mu       sync.Mutex
	webKey   string
	token    string
	tokenSeq int

	options synclayer.NetworkOption
	alg     []synclayer.ALGEntry
	upnp    synclayer.UPnP
	fw      synclayer.Firewall
	routes  []synclayer.StaticRouteIPv6
	nextID  int

	// events records the writes the fake accepted, in order.
	events []string
}

func newAdvancedFake() *advancedFake {
	return &advancedFake{
		webKey: "test-web-key",
		options: synclayer.NetworkOption{
			WANBlocking:    true,
			ICMPv6Blocking: true,
			NATTCPTimer:    3600,
			NATUDPTimer:    300,
			RemoteAccess:   synclayer.RemoteAccess{Active: false, Port: 8080},
			SecureAccess:   synclayer.RemoteAccess{Active: false, Port: 8080},
		},
		alg: []synclayer.ALGEntry{
			{ID: 0, DisplayName: "FTP", ServiceCode: "ftp", Active: true},
			{ID: 1, DisplayName: "TFTP", ServiceCode: "tftp", Active: false},
			{ID: 2, DisplayName: "SIP", ServiceCode: "sip", Active: true},
			{ID: 3, DisplayName: "RTSP", ServiceCode: "rtsp", Active: false},
		},
		upnp:   synclayer.UPnP{Active: true, Interval: 30, TTL: 2},
		fw:     synclayer.Firewall{IPv4: synclayer.FirewallIPv4{Active: true, Level: "low", Blocks: synclayer.FirewallBlocks{IPFlood: true}}, IPv6: synclayer.FirewallIPv6{Active: true}},
		nextID: 10,
	}
}

func (f *advancedFake) auth(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token == "" || r.Header.Get("Access-Token") != f.token {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":2003,"type":"invalid_token","message":"Invalid token"}}`))
		return false
	}
	return true
}

func (f *advancedFake) handler() http.Handler {
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

	mux.HandleFunc("/api/v1/service/networkOption", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(f.options)
		case http.MethodPut:
			// The device refuses a write that leaves a field out.
			var raw map[string]any
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			required := []string{"ipsecPassthrough", "pptpPassthrough", "l2tpPassthrough", "wanBlocking", "icmpv6Blocking", "natTcpTimer", "natUdpTimer", "remoteAccess", "secureAccess", "multicast"}
			for _, key := range required {
				if _, ok := raw[key]; !ok {
					http.Error(w, `{"error":{"code":500,"type":"internal_error"}}`, http.StatusInternalServerError)
					return
				}
			}
			var body synclayer.NetworkOption
			blob, _ := json.Marshal(raw)
			_ = json.Unmarshal(blob, &body)
			// The device keeps one management port for both services.
			if body.RemoteAccess.Port != f.options.RemoteAccess.Port {
				body.SecureAccess.Port = body.RemoteAccess.Port
			}
			f.options = body
			f.events = append(f.events, "options_put")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/service/alg", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(f.alg)
		case http.MethodPut:
			var body []synclayer.ALGEntry
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The device replaces the whole list: an entry left out is off.
			for i := range f.alg {
				f.alg[i].Active = false
			}
			for _, entry := range body {
				for i := range f.alg {
					if f.alg[i].ServiceCode == entry.ServiceCode {
						f.alg[i].Active = entry.Active
					}
				}
			}
			f.events = append(f.events, "alg_put")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/service/upnp", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(f.upnp)
		case http.MethodPut:
			// The device requires all three fields.
			var raw map[string]any
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			for _, key := range []string{"active", "interval", "ttl"} {
				if _, ok := raw[key]; !ok {
					http.Error(w, `{"error":{"code":3001,"type":"invalid_parameter"}}`, http.StatusBadRequest)
					return
				}
			}
			var body synclayer.UPnP
			blob, _ := json.Marshal(raw)
			_ = json.Unmarshal(blob, &body)
			f.upnp = body
			f.events = append(f.events, "upnp_put")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/security/firewall", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(f.fw)
		case http.MethodPut:
			var body synclayer.Firewall
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.fw = body
			f.events = append(f.events, "firewall_put")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	listRoutes6 := func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"active": false, "list": f.routes, "maxRules": 32})
		case http.MethodPost:
			var body synclayer.StaticRouteIPv6
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			body.ID = f.nextID
			f.nextID++
			body.Status = "Enabled"
			if body.Interface == 0 {
				body.IfName = "br0"
			}
			f.routes = append(f.routes, body)
			f.events = append(f.events, "route6_create")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": body.ID})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
	mux.HandleFunc("/api/v1/service/staticRouteIpv6", listRoutes6)

	mux.HandleFunc("/api/v1/service/staticRouteIpv6/", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		id, err := strconv.Atoi(r.URL.Path[len("/api/v1/service/staticRouteIpv6/"):])
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodPut:
			// The firmware implements an update as a delete followed by a
			// create, so the route comes back under a new id.
			var body synclayer.StaticRouteIPv6
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			kept := make([]synclayer.StaticRouteIPv6, 0, len(f.routes))
			for _, route := range f.routes {
				if route.ID != id {
					kept = append(kept, route)
				}
			}
			body.ID = f.nextID
			f.nextID++
			body.Status = "Enabled"
			if body.Interface == 0 {
				body.IfName = "br0"
			}
			f.routes = append(kept, body)
			f.events = append(f.events, "route6_update")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case http.MethodDelete:
			kept := make([]synclayer.StaticRouteIPv6, 0, len(f.routes))
			for _, route := range f.routes {
				if route.ID != id {
					kept = append(kept, route)
				}
			}
			f.routes = kept
			f.events = append(f.events, "route6_delete")
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	return mux
}

func (f *advancedFake) eventCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e == name {
			n++
		}
	}
	return n
}

func TestNetworkOptionsPartialWritesAndALG(t *testing.T) {
	f := newAdvancedFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			// This resource has no neutral value, so destroy must leave the
			// device as the last apply left it, and must not write anything.
			if f.options.NATTCPTimer != 7200 || !f.options.Multicast || f.options.NATUDPTimer != 300 {
				return fmt.Errorf("destroy changed the network options: %+v", f.options)
			}
			if n := f.eventCount("options_put"); n != 2 {
				return fmt.Errorf("destroy wrote the network options: options_put = %d", n)
			}
			enabled := make(map[string]bool, len(f.alg))
			for _, entry := range f.alg {
				enabled[entry.ServiceCode] = entry.Active
			}
			if !enabled["ftp"] || enabled["tftp"] || enabled["sip"] || enabled["rtsp"] {
				return fmt.Errorf("destroy changed the helper list: %+v", f.alg)
			}
			if n := f.eventCount("alg_put"); n != 1 {
				return fmt.Errorf("destroy wrote the ALG list: alg_put = %d", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Only some options are set, and ALG is not mentioned at all.
				// The whole options object still has to go out, and the helper
				// list must be left alone.
				Config: `
resource "synclayer_sxep200w_network_options" "t" {
  nat_tcp_timer  = 7200
  multicast      = true
}`,
				Check: func(_ *terraform.State) error {
					if f.eventCount("alg_put") != 0 {
						return fmt.Errorf("the ALG list was written although it is not configured")
					}
					if f.options.NATTCPTimer != 7200 {
						return fmt.Errorf("natTcpTimer = %d, want 7200", f.options.NATTCPTimer)
					}
					if !f.options.Multicast {
						return fmt.Errorf("multicast was not applied")
					}
					if f.options.NATUDPTimer != 300 || !f.options.WANBlocking {
						return fmt.Errorf("a field that was not configured was changed: %+v", f.options)
					}
					return nil
				},
			},
			{
				// Declaring the helpers writes the whole list: the ones left
				// out are switched off.
				Config: `
resource "synclayer_sxep200w_network_options" "t" {
  nat_tcp_timer = 7200
  multicast     = true
  alg_enabled   = ["ftp"]
}`,
				Check: func(_ *terraform.State) error {
					if f.eventCount("alg_put") != 1 {
						return fmt.Errorf("alg writes = %d, want 1", f.eventCount("alg_put"))
					}
					for _, entry := range f.alg {
						want := entry.ServiceCode == "ftp"
						if entry.Active != want {
							return fmt.Errorf("%s active = %v, want %v", entry.ServiceCode, entry.Active, want)
						}
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_network_options.t",
				ImportState:       true,
				ImportStateId:     networkOptionsID,
				ImportStateVerify: true,
			},
		},
	})
}

func TestFirewallLifecycle(t *testing.T) {
	f := newAdvancedFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			// Destroy must not switch protection off.
			if !f.fw.IPv4.Active || !f.fw.IPv6.Active {
				return fmt.Errorf("destroy changed the firewall: %+v", f.fw)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: `
resource "synclayer_sxep200w_firewall" "t" {
  ipv4_level                  = "medium"
  ipv4_block_port_scan_detection = true
}`,
				Check: func(_ *terraform.State) error {
					if f.fw.IPv4.Level != "medium" {
						return fmt.Errorf("level = %q, want medium", f.fw.IPv4.Level)
					}
					if !f.fw.IPv4.Blocks.PortScanDetection {
						return fmt.Errorf("port scan detection was not enabled")
					}
					// The field that was not configured keeps its value.
					if !f.fw.IPv4.Blocks.IPFlood || !f.fw.IPv4.Active || !f.fw.IPv6.Active {
						return fmt.Errorf("an unconfigured field changed: %+v", f.fw)
					}
					return nil
				},
			},
			{
				// The level and the block flags are independent.
				Config: `
resource "synclayer_sxep200w_firewall" "t" {
  ipv4_level                  = "low"
  ipv4_block_port_scan_detection = true
  ipv4_block_ip_spoofing      = true
}`,
				Check: func(_ *terraform.State) error {
					if f.fw.IPv4.Level != "low" {
						return fmt.Errorf("level = %q, want low", f.fw.IPv4.Level)
					}
					if !f.fw.IPv4.Blocks.PortScanDetection || !f.fw.IPv4.Blocks.IPSpoofing {
						return fmt.Errorf("blocks = %+v, want port scan and spoofing on", f.fw.IPv4.Blocks)
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_firewall.t",
				ImportState:       true,
				ImportStateId:     firewallID,
				ImportStateVerify: true,
			},
		},
	})
}

func TestUPnPLifecycle(t *testing.T) {
	f := newAdvancedFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			// UPnP has an off state, and the timings it was left with are kept.
			if f.upnp.Active {
				return fmt.Errorf("UPnP is still on after destroy")
			}
			if f.upnp.Interval != 60 || f.upnp.TTL != 2 {
				return fmt.Errorf("destroy changed the timings: %+v", f.upnp)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// Interval and TTL are not configured; the device still needs
				// them on every write.
				Config: `
resource "synclayer_sxep200w_upnp" "t" {
  active = false
}`,
				Check: func(_ *terraform.State) error {
					if f.upnp.Active {
						return fmt.Errorf("UPnP was not switched off")
					}
					if f.upnp.Interval != 30 || f.upnp.TTL != 2 {
						return fmt.Errorf("an unconfigured field changed: %+v", f.upnp)
					}
					return nil
				},
			},
			{
				Config: `
resource "synclayer_sxep200w_upnp" "t" {
  active   = true
  interval = 60
}`,
				Check: func(_ *terraform.State) error {
					if !f.upnp.Active || f.upnp.Interval != 60 || f.upnp.TTL != 2 {
						return fmt.Errorf("upnp = %+v, want on with interval 60 and ttl 2", f.upnp)
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_upnp.t",
				ImportState:       true,
				ImportStateId:     upnpID,
				ImportStateVerify: true,
			},
		},
	})
}

func TestStaticRouteIPv6Lifecycle(t *testing.T) {
	f := newAdvancedFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		CheckDestroy: func(_ *terraform.State) error {
			if len(f.routes) != 0 {
				return fmt.Errorf("routes left behind: %+v", f.routes)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: `
resource "synclayer_sxep200w_static_route_ipv6" "t" {
  destination_ip = "2001:db8:1::"
  prefix_length  = 64
  gateway        = "fe80::1"
}`,
				Check: func(_ *terraform.State) error {
					if len(f.routes) != 1 {
						return fmt.Errorf("routes = %d, want 1", len(f.routes))
					}
					if f.routes[0].PrefixLength != 64 {
						return fmt.Errorf("prefix length = %d, want 64", f.routes[0].PrefixLength)
					}
					return nil
				},
			},
			{
				// The update re-creates the route, so the id changes and the
				// resource has to follow it.
				Config: `
resource "synclayer_sxep200w_static_route_ipv6" "t" {
  destination_ip = "2001:db8:1::"
  prefix_length  = 48
  gateway        = "fe80::1"
}`,
				Check: func(_ *terraform.State) error {
					if len(f.routes) != 1 {
						return fmt.Errorf("routes = %d, want 1", len(f.routes))
					}
					if f.routes[0].PrefixLength != 48 {
						return fmt.Errorf("prefix length = %d, want 48", f.routes[0].PrefixLength)
					}
					return nil
				},
			},
			{
				ResourceName:      "synclayer_sxep200w_static_route_ipv6.t",
				ImportState:       true,
				ImportStateVerify: true,
				// The import id is the device id the resource holds at this
				// point.
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs, ok := s.RootModule().Resources["synclayer_sxep200w_static_route_ipv6.t"]
					if !ok {
						return "", fmt.Errorf("resource not found in state")
					}
					return rs.Primary.ID, nil
				},
			},
		},
	})

	if f.eventCount("route6_create") == 0 || f.eventCount("route6_delete") == 0 {
		t.Errorf("expected the route to be created and deleted, events: %v", f.events)
	}
}

func TestNetworkOptionsRejectsUnknownALG(t *testing.T) {
	f := newAdvancedFake()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	t.Setenv("SYNCLAYER_HOST", srv.URL)
	t.Setenv("SYNCLAYER_USERNAME", "admin")
	t.Setenv("SYNCLAYER_PASSWORD", "s3cret")

	resource.UnitTest(t, resource.TestCase{
		ProviderFactories: map[string]func() (*schema.Provider, error){
			"synclayer": func() (*schema.Provider, error) { return New(), nil },
		},
		Steps: []resource.TestStep{
			{
				// A code the device does not offer is refused rather than
				// silently dropped, which would leave a configuration that
				// never converges.
				Config: `
resource "synclayer_sxep200w_network_options" "t" {
  alg_enabled = ["ftpp"]
}`,
				ExpectError: regexp.MustCompile("unknown ALG service code"),
			},
		},
	})

	if f.eventCount("alg_put") != 0 {
		t.Errorf("the helper list was written despite an unknown code: %v", f.events)
	}
}

func TestStaticRouteIPv6PrefixLengthSchema(t *testing.T) {
	validate := resourceStaticRouteIPv6().Schema["prefix_length"].ValidateFunc
	if validate == nil {
		t.Fatal("prefix_length has no ValidateFunc")
	}
	// 0 is a default route, 128 a single address.
	for _, v := range []int{0, 1, 64, 128} {
		if _, errs := validate(v, "prefix_length"); len(errs) > 0 {
			t.Errorf("prefix_length %d should be accepted: %v", v, errs)
		}
	}
	for _, v := range []int{-1, 129} {
		if _, errs := validate(v, "prefix_length"); len(errs) == 0 {
			t.Errorf("prefix_length %d should be refused", v)
		}
	}
}
