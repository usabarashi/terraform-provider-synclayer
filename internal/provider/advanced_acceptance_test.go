package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	lan     synclayer.LAN
	nextID  int

	// Packet filter rules per address family, and port triggering rules. The
	// IPv4 family starts with one rule so tests can exercise the occupied
	// priority path.
	acRules  map[string][]synclayer.AccessControlRule
	triggers []synclayer.PortTriggeringRule
	nextTrig int

	// acInterleave makes another writer take a packet filter priority right
	// after the first read, and triggerDecoy makes a second port triggering
	// rule appear between the reads that identify a created one.
	acInterleave bool
	acGets       int
	triggerDecoy bool

	// acFailPost makes the packet filter write fail. For port triggering,
	// triggerFailAfterCreate makes the read that follows a create fail (once),
	// and triggerHideCreated makes those reads leave the created rule out.
	acFailPost             bool
	triggerFailAfterCreate bool
	triggerListFails       int
	triggerHideCreated     bool

	// eco and date are the device's management settings. The device answers an
	// eco write asynchronously, so ecoPending holds a change that only becomes
	// visible after ecoPendingReads reads.
	eco             synclayer.EcoMode
	ecoPending      *synclayer.EcoMode
	ecoPendingReads int
	ecoNeverApplies bool
	date            synclayer.DateTime

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
		upnp: synclayer.UPnP{Active: true, Interval: 30, TTL: 2},
		fw:   synclayer.Firewall{IPv4: synclayer.FirewallIPv4{Active: true, Level: "low", Blocks: synclayer.FirewallBlocks{IPFlood: true}}, IPv6: synclayer.FirewallIPv6{Active: true}},
		lan: synclayer.LAN{
			MACAddress: "02:00:00:00:00:01",
			IPv4: synclayer.LANIPv4{
				IPAddress: "192.168.0.1",
				Subnet:    "255.255.255.0",
				DHCP: synclayer.LANDHCP{
					Active:     true,
					StartIP:    "192.168.0.100",
					EndIP:      "192.168.0.149",
					LeaseTime:  86400,
					WinsServer: "0.0.0.0",
					Assignment: "manual",
				},
			},
			IPv6: map[string]any{"mode": "stateless"},
		},
		nextID: 10,
		acRules: map[string][]synclayer.AccessControlRule{
			"ipv4": {{
				Index: 1, Active: true, Description: "", FilterType: "deny", Target: 1, Protocol: "tcp",
				Src:  synclayer.AccessControlEndpoint{Type: "any", IPAddress: "", StartPort: 0, EndPort: 0},
				Dest: synclayer.AccessControlEndpoint{Type: "any", IPAddress: "", StartPort: 137, EndPort: 139},
			}},
			"ipv6": {},
		},
		nextTrig: 1,
		eco:      synclayer.EcoMode{Active: false, Type: 1, ScheduleEnabled: false, StartTime: "21:00", EndTime: "06:00"},
		date: synclayer.DateTime{
			DaylightSaving: synclayer.DateTimeDST{Active: false},
			NTP:            synclayer.DateTimeNTP{Active: true, Servers: []string{"ntp.example.test"}},
			TimeZone:       127,
			TimeZoneName:   "Tokyo",
		},
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

	mux.HandleFunc("/api/v1/network/lan", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(f.lan)
		case http.MethodPut:
			var body synclayer.LAN
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The device replaces the whole object, keeping every field the
			// write carries, including the IPv6 block it does not expose.
			f.lan = body
			f.events = append(f.events, "lan_put")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/security/accessControl", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		family := r.URL.Query().Get("filter")
		if family != "ipv4" && family != "ipv6" {
			http.Error(w, "unsupported filter", http.StatusBadRequest)
			return
		}
		rules := f.acRules[family]
		_ = json.NewEncoder(w).Encode(map[string]any{
			family: map[string]any{"active": true, "maxRules": 32, "rules": rules},
		})

		f.acGets++
		if f.acInterleave && f.acGets == 1 {
			// Another writer takes priority 20 between the checks the provider
			// makes before writing.
			f.acRules[family] = append(f.acRules[family], synclayer.AccessControlRule{
				Index: 20, Active: true, Description: "someone else", FilterType: "deny", Protocol: "tcp",
			})
		}
	})
	mux.HandleFunc("/api/v1/security/accessControl/", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}

		parts := strings.SplitN(r.URL.Path[len("/api/v1/security/accessControl/"):], "/", 2)
		family := parts[0]
		if family != "ipv4" && family != "ipv6" {
			http.Error(w, "unsupported family", http.StatusBadRequest)
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		drop := func(index int) {
			kept := make([]synclayer.AccessControlRule, 0, len(f.acRules[family]))
			for _, existing := range f.acRules[family] {
				if existing.Index != index {
					kept = append(kept, existing)
				}
			}
			f.acRules[family] = kept
		}

		switch {
		case len(parts) == 1 && r.Method == http.MethodPost:
			if f.acFailPost {
				http.Error(w, `{"error":{"code":500,"type":"internal_error","message":"simulated failure"}}`, http.StatusInternalServerError)
				return
			}
			var rule synclayer.AccessControlRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The device replaces whatever sits at the index, and stores
			// nothing at all when the rule is not active.
			if !rule.Active {
				_ = json.NewEncoder(w).Encode(map[string]any{})
				return
			}
			drop(rule.Index)
			f.acRules[family] = append(f.acRules[family], rule)
			f.events = append(f.events, "access_control_create")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case len(parts) == 2 && r.Method == http.MethodPut:
			index, err := strconv.Atoi(parts[1])
			if err != nil {
				http.Error(w, "invalid index", http.StatusBadRequest)
				return
			}
			var rule synclayer.AccessControlRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// An index that is free is created; active=false removes the rule.
			rule.Index = index
			drop(index)
			if rule.Active {
				f.acRules[family] = append(f.acRules[family], rule)
			}
			f.events = append(f.events, "access_control_update")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case len(parts) == 2 && r.Method == http.MethodDelete:
			index, err := strconv.Atoi(parts[1])
			if err != nil {
				http.Error(w, "invalid index", http.StatusBadRequest)
				return
			}
			drop(index)
			f.events = append(f.events, "access_control_delete")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/service/portTriggering", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			if f.triggerListFails > 0 {
				f.triggerListFails--
				http.Error(w, `{"error":{"code":500,"type":"internal_error","message":"simulated failure"}}`, http.StatusInternalServerError)
				return
			}
			rules := f.triggers
			if f.triggerHideCreated && len(rules) > 0 {
				rules = rules[:len(rules)-1]
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"rules": rules, "active": true, "maxRules": 10})
		case http.MethodPost:
			var rule synclayer.PortTriggeringRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rule.ID = f.nextTrig
			f.nextTrig++
			f.triggers = append(f.triggers, rule)
			if f.triggerDecoy {
				// Another create lands between the two reads.
				f.triggers = append(f.triggers, synclayer.PortTriggeringRule{
					ID:          f.nextTrig,
					Active:      true,
					Description: "someone else",
					Triggered:   synclayer.PortTriggeringRange{Protocol: "udp", StartRange: 1234, EndRange: 1234},
					Forwarded:   synclayer.PortTriggeringRange{Protocol: "udp", StartRange: 1235, EndRange: 1235},
				})
				f.nextTrig++
			}
			f.events = append(f.events, "trigger_create")
			if f.triggerFailAfterCreate {
				f.triggerListFails = 1
			}
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/service/portTriggering/", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}

		rest := r.URL.Path[len("/api/v1/service/portTriggering/"):]

		f.mu.Lock()
		defer f.mu.Unlock()

		if rest == "active" {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			f.events = append(f.events, "trigger_active")
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}

		id, err := strconv.Atoi(rest)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodPut:
			var rule synclayer.PortTriggeringRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// A disabled rule stays in the list, unlike a packet filter rule.
			rule.ID = id
			for i := range f.triggers {
				if f.triggers[i].ID == id {
					f.triggers[i] = rule
				}
			}
			f.events = append(f.events, "trigger_update")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case http.MethodDelete:
			kept := make([]synclayer.PortTriggeringRule, 0, len(f.triggers))
			for _, rule := range f.triggers {
				if rule.ID != id {
					kept = append(kept, rule)
				}
			}
			f.triggers = kept
			f.events = append(f.events, "trigger_delete")
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/gateway/eco", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			// A write is applied asynchronously: the old settings are reported
			// until it has been taken up.
			if f.ecoPending != nil && !f.ecoNeverApplies {
				f.ecoPendingReads--
				if f.ecoPendingReads <= 0 {
					f.eco = *f.ecoPending
					f.ecoPending = nil
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"eco": f.eco, "options": map[string]any{}})
		case http.MethodPut:
			var body struct {
				Eco synclayer.EcoMode `json:"eco"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// The device accepts the write asynchronously: it is applied only
			// after a couple of reads, like a deferred job.
			f.ecoPending = &body.Eco
			f.ecoPendingReads = 2
			f.events = append(f.events, "eco_put")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/v1/gateway/datetime", func(w http.ResponseWriter, r *http.Request) {
		if !f.auth(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"currentDateTime":    "2026.09.27 21:18:07",
				"daylightSavingTime": f.date.DaylightSaving,
				"ntp":                f.date.NTP,
				"timeZone":           f.date.TimeZone,
				"timeZoneName":       f.date.TimeZoneName,
				"timeZoneList":       []string{"(GMT+09:00) Tokyo"},
			})
		case http.MethodPut:
			var body synclayer.DateTime
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.date = body
			f.events = append(f.events, "datetime_put")
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
