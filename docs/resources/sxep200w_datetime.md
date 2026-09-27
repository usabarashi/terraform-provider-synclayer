---
page_title: "synclayer_sxep200w_datetime Resource"
description: |-
  Manages the date and time settings (NTP, time zone, daylight saving) of the SXEP200W.
---

# synclayer_sxep200w_datetime

Manages the device's date and time settings: NTP, time zone and daylight
saving.

The device's current time is deliberately not part of this resource. It comes
from NTP, and a clock is not a configuration value: exposing it would mean a
state that changes on every read. Destroying this resource removes it from state
and leaves the device as it is, because there is no neutral time zone or clock to
write back.

## Example Usage

```terraform
resource "synclayer_sxep200w_datetime" "main" {
  ntp_active  = true
  ntp_servers = ["ntp.nict.jp"]
  time_zone   = 127
}
```

## Argument Reference

- `ntp_active` (Boolean, Optional) Whether the device takes its time from NTP.
- `ntp_servers` (List of String, Optional) NTP servers, in the order the device
  should use them.
- `time_zone` (Number, Optional) Time zone, as the index the device uses.
- `daylight_saving_active` (Boolean, Optional) Whether daylight saving is
  enabled.
- `daylight_saving_start` (String, Optional) Start of daylight saving, in the
  format the device expects.
- `daylight_saving_end` (String, Optional) End of daylight saving.

Fields that are not set are left at their current device value.

## Attributes Reference

In addition to the arguments above:

- `time_zone_name` (String) Name the device reports for `time_zone`, e.g.
  `Tokyo`.

## Management notes

- The write replaces the whole object. The device also reports the current time
  and the list of every time zone it knows; neither is sent back, and the device
  accepts a body without them.
- `time_zone` is the numeric index the device stores, not a name: the web UI
  shows the matching name from its own list. `time_zone_name` reports what the
  device calls the configured zone, which makes the index readable.
- The daylight saving dates are passed through as strings, in whatever format
  the device expects, since nothing here establishes it.
- Destroying the resource leaves the settings as they are.

## Import

Import using the fixed id `datetime`:

```sh
terraform import synclayer_sxep200w_datetime.main datetime
```
