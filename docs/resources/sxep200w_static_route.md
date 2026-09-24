---
page_title: "synclayer_sxep200w_static_route Resource"
description: |-
  Manages an IPv4 static route.
---

# synclayer_sxep200w_static_route

Manages an IPv4 static route on the device.

## Example Usage

```terraform
resource "synclayer_sxep200w_static_route" "lab" {
  destination_ip = "10.10.0.0"
  subnet         = "255.255.0.0"
  gateway        = "192.168.0.1"
  interface      = "wan"
  interface_name = "veip0.1"
  active         = true
}
```

## Argument Reference

- `destination_ip` (String, Required) Destination IPv4 network address.
- `subnet` (String, Required) Destination subnet mask, e.g. `255.255.255.0`.
- `gateway` (String, Required) Next-hop IPv4 address. Must be inside the LAN.
- `interface` (String, Optional) Egress interface, `lan` or `wan`. Defaults to
  `lan`.
- `interface_name` (String, Optional) Concrete interface name (e.g.
  `veip0.1`). Required when `interface` is `wan`; must be omitted for `lan`.
- `active` (Boolean, Optional) Whether the route is enabled. Defaults to
  `true`.

## Attributes Reference

In addition to the arguments above:

- `id` (String) Numeric route id assigned by the device.

## Import

Static routes can be imported using the device route id:

```sh
terraform import synclayer_sxep200w_static_route.lab 3
```
