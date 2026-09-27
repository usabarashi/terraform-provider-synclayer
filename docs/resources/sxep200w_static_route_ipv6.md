---
page_title: "synclayer_sxep200w_static_route_ipv6 Resource"
description: |-
  Manages an IPv6 static route on the SXEP200W.
---

# synclayer_sxep200w_static_route_ipv6

Manages an IPv6 static route on the device.

Routes are identified by the numeric id assigned by the device; the resource can
be imported with that id. The IPv6 table takes a prefix length where the IPv4
table takes a netmask.

## Example Usage

```terraform
resource "synclayer_sxep200w_static_route_ipv6" "lan" {
  destination_ip = "2001:db8:1::"
  prefix_length  = 64
  gateway        = "fe80::1"
}

resource "synclayer_sxep200w_static_route_ipv6" "wan" {
  destination_ip = "2001:db8:2::"
  prefix_length  = 48
  gateway        = "fe80::2"
  interface      = "wan"
  interface_name = "veip0.1"
}
```

## Argument Reference

- `active` (Boolean, Optional) Whether the route is enabled. Defaults to `true`.
- `destination_ip` (String, Required) Destination IPv6 network address.
- `prefix_length` (Number, Required) Prefix length of the destination network,
  e.g. `64`.
- `gateway` (String, Required) Next-hop IPv6 address.
- `interface` (String, Optional) Egress interface: `lan` or `wan`. Defaults to
  `lan`.
- `interface_name` (String, Optional) Concrete interface name (e.g. `veip0.1`).
  Required when `interface` is `wan`; must be omitted for `lan`.

## Management notes

- The firmware implements an update as a delete followed by a create, so the
  device id changes on every update; the resource follows the new id.
- `destination_ip` carries the network address only: the prefix length is the
  `prefix_length` argument. A destination written with an embedded prefix is not
  interpreted as one.
- Destroying the resource removes the route; it does not touch the table's own
  on/off state or the other family.

## Import

Import using the numeric id the device assigned:

```sh
terraform import synclayer_sxep200w_static_route_ipv6.lan 26
```
