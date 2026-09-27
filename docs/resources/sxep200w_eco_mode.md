---
page_title: "synclayer_sxep200w_eco_mode Resource"
description: |-
  Manages the eco mode of the SXEP200W.
---

# synclayer_sxep200w_eco_mode

Manages the device's eco mode.

Eco mode can be switched on for its own schedule (a start and an end time),
which is disabled along with it when this resource is destroyed.

## Example Usage

```terraform
resource "synclayer_sxep200w_eco_mode" "main" {
  active           = true
  schedule_enabled = true
  start_time       = "23:00"
  end_time         = "06:00"
}
```

## Argument Reference

- `active` (Boolean, Optional) Whether eco mode is enabled.
- `type` (Number, Optional) Eco mode type, as the device reports it.
- `schedule_enabled` (Boolean, Optional) Whether eco mode follows its schedule.
- `start_time` (String, Optional) Start of the schedule, as `HH:MM`.
- `end_time` (String, Optional) End of the schedule, as `HH:MM`.

Fields that are not set are left at their current device value.

## Management notes

- The device replaces the whole object on every write, so the provider reads it
  first and changes only what is configured. A write is accepted asynchronously
  (HTTP 202) and the provider reads the result back.
- `type` is passed through as the device reports it. The device's web UI offers
  a set of modes; the numbering is not documented, so nothing here validates it.
- Destroying the resource turns eco mode off **and disables its schedule**, so a
  schedule cannot be left behind to run the next time it is switched on.

## Import

Import using the fixed id `eco_mode`:

```sh
terraform import synclayer_sxep200w_eco_mode.main eco_mode
```
