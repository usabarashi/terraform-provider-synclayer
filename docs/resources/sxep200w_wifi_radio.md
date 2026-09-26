---
page_title: "synclayer_sxep200w_wifi_radio Resource"
description: |-
  Manages the radio settings of a WiFi band on the SXEP200W.
---

# synclayer_sxep200w_wifi_radio

Manages the radio settings of a WiFi band.

Each band (`2.4g`, `5g`) has exactly one radio. Destroying this resource
**turns the radio off** rather than removing it, because the radio cannot be
deleted.

## Example Usage

```terraform
resource "synclayer_sxep200w_wifi_radio" "five_g" {
  band         = "5g"
  active       = true
  channel      = "auto"
  bandwidth    = "80"
  output_power = "high"
}
```

## Argument Reference

- `band` (String, Required) Radio band: `2.4g` or `5g`. Changing this forces a
  new resource.
- `active` (Boolean, Optional) Whether the radio is enabled.
- `wireless_mode` (String, Optional) Supported 802.11 modes, e.g.
  `802.11b+g+n+ax`.
- `channel` (String, Optional) Channel number, or `auto`.
- `bandwidth` (String, Optional) Channel bandwidth in MHz, e.g. `20`, `40`, `80`.
- `output_power` (String, Optional) Transmit power level, e.g. `high`, `medium`,
  `low`.

Fields that are not set are left at their current device value.

## Management notes

- Only the attributes you set are written; everything else keeps its current
  value.
- Destroying the resource turns the radio **off**; a band's radio cannot be
  removed. This affects every SSID on that band.

## Import

Import using the band name:

```sh
terraform import synclayer_sxep200w_wifi_radio.five_g 5g
```
