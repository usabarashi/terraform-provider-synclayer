---
page_title: "synclayer_sxep200w_reserved_ip Resource"
description: |-
  Manages a DHCP reserved IP address (static lease).
---

# synclayer_sxep200w_reserved_ip

Manages a DHCP reserved IP address (static lease) on the device.

## Example Usage

```terraform
resource "synclayer_sxep200w_reserved_ip" "nas" {
  mac_address = "AA:BB:CC:DD:EE:FF"
  ip_address  = "192.168.0.20"
  device_name = "nas"
}
```

## Argument Reference

- `mac_address` (String, Required) MAC address of the client. Changing this
  forces a new reservation.
- `ip_address` (String, Required) IPv4 address to assign to the client.
- `device_name` (String, Optional) Human readable label. May be normalised by
  the device.

## Attributes Reference

In addition to the arguments above:

- `id` (String) Numeric reservation id assigned by the device.

## Import

Reservations can be imported using the device reservation id:

```sh
terraform import synclayer_sxep200w_reserved_ip.nas 1
```
