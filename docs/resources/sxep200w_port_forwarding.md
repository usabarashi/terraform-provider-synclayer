---
page_title: "synclayer_sxep200w_port_forwarding Resource"
description: |-
  Manages a port forwarding (NAPT) rule.
---

# synclayer_sxep200w_port_forwarding

Manages a port forwarding (NAPT) rule on the device.

## Example Usage

```terraform
resource "synclayer_sxep200w_port_forwarding" "ssh" {
  service_type        = "SSH"
  ip_address          = "192.168.0.10"
  protocol            = "tcp"
  local_port_start    = 22
  local_port_end      = 22
  external_port_start = 2222
  external_port_end   = 2222
  active              = true
}
```

## Argument Reference

- `service_type` (String, Required) Free-form label for the rule, e.g. `HTTP`.
- `ip_address` (String, Required) LAN IPv4 address that receives the traffic.
- `protocol` (String, Required) One of `tcp`, `udp` or `tcp/udp`.
- `local_port_start` (Number, Required) First destination (LAN) port.
- `local_port_end` (Number, Required) Last destination (LAN) port.
- `external_port_start` (Number, Required) First source (WAN) port.
- `external_port_end` (Number, Required) Last source (WAN) port.
- `active` (Boolean, Optional) Whether the rule is enabled. Defaults to `true`.

## Attributes Reference

In addition to the arguments above:

- `id` (String) Numeric rule id assigned by the device.

## Import

Port forwarding rules can be imported using the device rule id:

```sh
terraform import synclayer_sxep200w_port_forwarding.ssh 4
```
