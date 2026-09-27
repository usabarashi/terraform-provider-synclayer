package synclayer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------
// Packet filtering (/security/accessControl)
//
// The device keeps one rule list per address family. A rule is addressed by its
// index inside that list, which the user picks when the rule is created: the
// same index is used for reads, updates and deletes, and nothing renumbers the
// remaining rules when one is removed (verified on VER-01.06.05-EA).
//
// Two behaviours matter to callers, both verified on the same firmware:
//
//   - a create that names an index that is already taken replaces the rule
//     living there, so callers must check the slot before creating;
//   - a write carrying active=false is accepted (HTTP 200) and does nothing at
//     all, because a rule has no disabled state: it exists and filters, or it
//     does not exist.
// ---------------------------------------------------------------------------

// AccessControlEndpoint is one side (source or destination) of a rule.
type AccessControlEndpoint struct {
	Type      string `json:"type"`
	IPAddress string `json:"ipAddress"`
	StartPort int    `json:"startPort"`
	EndPort   int    `json:"endPort"`
}

// AccessControlRule mirrors one entry of GET /security/accessControl.
type AccessControlRule struct {
	Index int `json:"index"`

	// Active is always true for a rule the device holds; see the note above.
	Active      bool                  `json:"active"`
	Description string                `json:"description"`
	FilterType  string                `json:"filterType"`
	Target      int                   `json:"target"`
	Protocol    string                `json:"protocol"`
	ICMPType    string                `json:"icmpType"`
	Src         AccessControlEndpoint `json:"src"`
	Dest        AccessControlEndpoint `json:"dest"`
}

type accessControlFamily struct {
	Active   bool                `json:"active"`
	MaxRules int                 `json:"maxRules"`
	Rules    []AccessControlRule `json:"rules"`
}

type accessControlResponse struct {
	IPv4 *accessControlFamily `json:"ipv4,omitempty"`
	IPv6 *accessControlFamily `json:"ipv6,omitempty"`
}

// ListAccessControlRules returns the packet filter rules of one family
// ("ipv4" or "ipv6").
func (c *Client) ListAccessControlRules(ctx context.Context, family string) ([]AccessControlRule, error) {
	var resp accessControlResponse
	if err := c.request(ctx, http.MethodGet, "/security/accessControl?filter="+family, nil, &resp); err != nil {
		return nil, err
	}
	switch family {
	case "ipv4":
		if resp.IPv4 == nil {
			return nil, nil
		}
		return resp.IPv4.Rules, nil
	case "ipv6":
		if resp.IPv6 == nil {
			return nil, nil
		}
		return resp.IPv6.Rules, nil
	default:
		return nil, fmt.Errorf("unsupported address family %q (expected \"ipv4\" or \"ipv6\")", family)
	}
}

// GetAccessControlRule returns a rule by its index, or (nil, nil) when the
// index is free.
func (c *Client) GetAccessControlRule(ctx context.Context, family string, index int) (*AccessControlRule, error) {
	rules, err := c.ListAccessControlRules(ctx, family)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].Index == index {
			return &rules[i], nil
		}
	}
	return nil, nil
}

// ErrAccessControlPriorityTaken reports that the requested packet filter index
// is in use. Nothing was written when it is returned.
var ErrAccessControlPriorityTaken = errors.New("packet filter priority is already in use")

// CreateAccessControlRule adds a rule at the index it carries.
//
// The device replaces whatever already sits at that index and offers nothing
// like a conditional create: an If-None-Match header is ignored and the write
// goes through anyway (checked on VER-01.06.05-EA). The slot is therefore
// checked and taken while holding a lock, so two creates in the same provider
// cannot both see it free and have the second overwrite the first. A writer
// outside this process can still win that race, and no request the device
// accepts would make the pair atomic.
func (c *Client) CreateAccessControlRule(ctx context.Context, family string, rule AccessControlRule) error {
	if !rule.Active {
		// A rule that is not active is a rule that does not exist: the device
		// would answer 200 and store nothing.
		return errors.New("a packet filter rule cannot be created inactive: the device has no disabled state for rules")
	}

	c.accessControlMu.Lock()
	defer c.accessControlMu.Unlock()

	existing, err := c.GetAccessControlRule(ctx, family, rule.Index)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("%w: index %d is used by a rule described as %q, so it is not taken over here: import it instead", ErrAccessControlPriorityTaken, rule.Index, existing.Description)
	}
	return c.request(ctx, http.MethodPost, "/security/accessControl/"+family, rule, nil)
}

// UpdateAccessControlRule replaces the rule at an index.
func (c *Client) UpdateAccessControlRule(ctx context.Context, family string, index int, rule AccessControlRule) error {
	rule.Index = index
	if !rule.Active {
		// active=false removes the rule instead of disabling it.
		return c.DeleteAccessControlRule(ctx, family, index)
	}
	return c.request(ctx, http.MethodPut, "/security/accessControl/"+family+"/"+strconv.Itoa(index), rule, nil)
}

// DeleteAccessControlRule removes the rule at an index.
func (c *Client) DeleteAccessControlRule(ctx context.Context, family string, index int) error {
	return c.request(ctx, http.MethodDelete, "/security/accessControl/"+family+"/"+strconv.Itoa(index), nil, nil)
}

// ---------------------------------------------------------------------------
// Port triggering (/service/portTriggering)
// ---------------------------------------------------------------------------

// PortTriggeringRange is one side of a port triggering rule.
type PortTriggeringRange struct {
	Protocol   string `json:"protocol"`
	StartRange int    `json:"startRange"`
	EndRange   int    `json:"endRange"`
}

// PortTriggeringRule mirrors one entry of GET /service/portTriggering. Unlike a
// packet filter rule it does have a disabled state: active=false keeps it in
// the list (verified on VER-01.06.05-EA).
type PortTriggeringRule struct {
	ID          int                 `json:"id,omitempty"`
	Active      bool                `json:"active"`
	Triggered   PortTriggeringRange `json:"triggered"`
	Forwarded   PortTriggeringRange `json:"forwarded"`
	Description string              `json:"description"`
}

type portTriggeringList struct {
	Rules    []PortTriggeringRule `json:"rules"`
	Active   bool                 `json:"active"`
	MaxRules int                  `json:"maxRules"`
}

// ListPortTriggeringRules returns every configured port triggering rule.
func (c *Client) ListPortTriggeringRules(ctx context.Context) ([]PortTriggeringRule, error) {
	var resp portTriggeringList
	if err := c.request(ctx, http.MethodGet, "/service/portTriggering", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Rules, nil
}

// GetPortTriggeringRule returns a rule by id.
func (c *Client) GetPortTriggeringRule(ctx context.Context, id int) (*PortTriggeringRule, error) {
	rules, err := c.ListPortTriggeringRules(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i], nil
		}
	}
	return nil, nil
}

// CreatePortTriggeringRule adds a rule and returns its device-assigned id.
//
// The device does not report the id it assigned, so the rule is found again by
// comparing what appeared against what was asked for. The whole discovery is
// serialised: two creates running at the same time would otherwise see each
// other's rules and fail to tell which id is theirs.
//
// The read that identifies the new rule can fail on its own — by then the rule
// exists — so it is retried a few times before giving up, and the error it
// returns says that the rule may have to be imported rather than pretending
// nothing happened.
func (c *Client) CreatePortTriggeringRule(ctx context.Context, rule PortTriggeringRule) (int, error) {
	c.portTriggeringMu.Lock()
	defer c.portTriggeringMu.Unlock()

	before, err := c.ListPortTriggeringRules(ctx)
	if err != nil {
		return 0, err
	}
	if err := c.request(ctx, http.MethodPost, "/service/portTriggering", rule, nil); err != nil {
		return 0, err
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}

		after, err := c.ListPortTriggeringRules(ctx)
		if err != nil {
			lastErr = err
			continue
		}

		seen := make(map[int]bool, len(before))
		for _, r := range before {
			seen[r.ID] = true
		}
		newID := 0
		matches := 0
		for _, r := range after {
			if seen[r.ID] || !samePortTriggeringRule(r, rule) {
				continue
			}
			newID = r.ID
			matches++
		}
		if matches == 1 {
			return newID, nil
		}
		lastErr = errors.New("the rule could not be told apart from the others on the device")
	}

	return 0, fmt.Errorf("the rule was created but its id could not be read back (%v): check the device and import the rule before applying again", lastErr)
}

// samePortTriggeringRule reports whether two rules describe the same thing. The
// device assigns the id, so it is ignored.
func samePortTriggeringRule(a, b PortTriggeringRule) bool {
	return a.Active == b.Active &&
		a.Description == b.Description &&
		a.Triggered == b.Triggered &&
		a.Forwarded == b.Forwarded
}

// UpdatePortTriggeringRule replaces a rule.
func (c *Client) UpdatePortTriggeringRule(ctx context.Context, id int, rule PortTriggeringRule) error {
	rule.ID = id
	return c.request(ctx, http.MethodPut, "/service/portTriggering/"+strconv.Itoa(id), rule, nil)
}

// DeletePortTriggeringRule removes a rule.
func (c *Client) DeletePortTriggeringRule(ctx context.Context, id int) error {
	return c.request(ctx, http.MethodDelete, "/service/portTriggering/"+strconv.Itoa(id), nil, nil)
}

// SetPortTriggeringActive turns port triggering on or off. The switch belongs
// to the feature, not to any single rule.
func (c *Client) SetPortTriggeringActive(ctx context.Context, active bool) error {
	return c.request(ctx, http.MethodPost, "/service/portTriggering/active", map[string]any{"active": active}, nil)
}
