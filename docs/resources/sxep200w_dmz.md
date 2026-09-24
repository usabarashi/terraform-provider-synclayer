---
page_title: "synclayer_sxep200w_dmz Resource"
description: |-
  Manages the DMZ host of the device.
---

# synclayer_sxep200w_dmz

Manages the DMZ host of the device.

This is a **singleton** resource: there is only one DMZ host per device.
Destroying the resource disables the DMZ rather than removing it.

## Example Usage

```terraform
resource "synclayer_sxep200w_dmz" "game_console" {
  active      = true
  destination = "192.168.0.30"
  subnet      = "255.255.255.0"
}
```

## Argument Reference

- `active` (Boolean, Optional) Whether the DMZ host is enabled. Defaults to
  `true`.
- `destination` (String, Optional) LAN IPv4 address that receives all
  unsolicited inbound traffic. Meaningful when `active` is `true`; the device
  reports `0.0.0.0` while disabled.
- `subnet` (String, Optional) Subnet mask associated with the destination.

## Import

The singleton can be imported with the id `dmz`:

```sh
terraform import synclayer_sxep200w_dmz.game_console dmz
```
