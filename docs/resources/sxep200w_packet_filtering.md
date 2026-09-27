---
page_title: "synclayer_sxep200w_packet_filtering Resource"
description: |-
  Manages one packet filter rule on the SXEP200W.
---

# synclayer_sxep200w_packet_filtering

Manages one packet filter rule.

Rules are addressed by an address family and a priority, which the device uses as
the rule's index in that family's list. The device ships with a list of default
rules in the IPv4 family (NetBIOS, SMB, NFS and similar): they are **not managed
here**, and a rule is never created on a priority they already occupy.

## Example Usage

```terraform
resource "synclayer_sxep200w_packet_filtering" "block_telnet" {
  family      = "ipv4"
  priority    = 20
  filter_type = "deny"
  protocol    = "tcp"
  description = "block telnet from the LAN"

  src {
    type = "any"
  }

  dest {
    type       = "any"
    start_port = 23
    end_port   = 23
  }
}
```

## Argument Reference

- `family` (String, Required) Address family: `ipv4` or `ipv6`. Changing this
  forces a new resource.
- `priority` (Number, Required) Position of the rule, `1`-`32`. This is the
  index the device stores the rule under, so changing it forces a new resource.
- `filter_type` (String, Optional) `deny` or `allow`.
- `target` (Number, Optional) Direction the rule applies to, as the device
  reports it (`0` or `1`; its web UI calls the field the target interface).
- `protocol` (String, Optional) Protocol to match, e.g. `tcp`, `udp` or `icmp`.
- `icmp_type` (String, Optional) ICMP type when `protocol` is `icmp`.
- `description` (String, Optional) Label stored with the rule.
- `src` (Block, Optional) Source side, with `type`, `ip_address`, `start_port`
  and `end_port`.
- `dest` (Block, Optional) Destination side, same fields.

Fields that are not set are left at their current device value.

## Management notes

- **A rule has no disabled state.** The device keeps a rule or does not have it:
  writing one as inactive is accepted with a successful response and stores
  nothing. This resource therefore has no `active` argument, and destroying it
  removes the rule, which is the only way to switch it off.
- **A priority that is already in use is refused, not overwritten.** The device
  itself would replace whatever sits at that index, which would quietly take over
  a rule this configuration does not own — the factory rules included. Import
  the rule instead, or choose a free priority.
- The switch that turns packet filtering on and off for a whole family is not
  managed here; neither are the device's own rules.
- The device's IPv6 family is empty on this firmware; the same resource manages
  both families.
- Import with the id `<family>/<priority>`, for example `ipv4/20`.

## Import

```sh
terraform import synclayer_sxep200w_packet_filtering.block_telnet ipv4/20
```
