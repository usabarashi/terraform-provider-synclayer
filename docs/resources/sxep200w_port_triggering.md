---
page_title: "synclayer_sxep200w_port_triggering Resource"
description: |-
  Manages one port triggering rule on the SXEP200W.
---

# synclayer_sxep200w_port_triggering

Manages one port triggering rule.

A rule opens its forwarded range while traffic is seen on its triggered range.
Rules are identified by the id the device assigns, so the resource can be
imported with that id. The switch that turns port triggering on and off for the
whole feature is not managed here.

## Example Usage

```terraform
resource "synclayer_sxep200w_port_triggering" "game" {
  description = "game console"

  triggered {
    protocol    = "tcp"
    start_range = 59998
    end_range   = 59998
  }

  forwarded {
    protocol    = "tcp"
    start_range = 59997
    end_range   = 59997
  }
}
```

## Argument Reference

- `active` (Boolean, Optional) Whether the rule is enabled. Defaults to `true`.
- `description` (String, Optional) Label stored with the rule.
- `triggered` (Block, Required) Ports whose traffic arms the rule, with
  `protocol` (default `tcp`), `start_range` and `end_range` (`1`-`65535`).
- `forwarded` (Block, Required) Ports opened while the rule is armed, with the
  same fields.

## Management notes

- Unlike a packet filter rule, a port triggering rule does have a disabled
  state: setting `active = false` keeps it in the device's list.
- The device assigns the id, and does not report it on creation; the provider
  finds the rule again to learn it.
- Destroying the resource removes the rule.
- The feature-wide on/off switch is not managed here, so a configuration that
  manages rules does not enable or disable port triggering as a whole.

## Import

```sh
terraform import synclayer_sxep200w_port_triggering.game 1
```
