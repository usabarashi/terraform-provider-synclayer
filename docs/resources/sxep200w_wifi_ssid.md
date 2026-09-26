---
page_title: "synclayer_sxep200w_wifi_ssid Resource"
description: |-
  Manages a WiFi SSID (network) on the SXEP200W.
---

# synclayer_sxep200w_wifi_ssid

Manages a single WiFi SSID (network) on the device.

SSIDs are identified by a band and a slot index. Slots cannot be created or
removed, so destroying this resource **disables** the SSID rather than deleting
it.

## Example Usage

```terraform
resource "synclayer_sxep200w_wifi_ssid" "primary_5g" {
  band          = "5g"
  index         = 0
  active        = true
  name          = "home-5g"
  security_type = "WPA3-SAE/WPA2-PSK"
  password      = var.wifi_password
}

variable "wifi_password" {
  type      = string
  sensitive = true
}
```

## Argument Reference

- `band` (String, Required) Radio band: `2.4g` or `5g`. Changing this forces a
  new resource.
- `index` (Number, Required) SSID slot index on the band: `0` (primary) or `1`
  (secondary). No other slots can be managed — the device reports extra slots
  on some firmware revisions (e.g. `5g/2`) but its web UI does not expose them.
  Changing this forces a new resource.
- `name` (String, Required) Network name (SSID).
- `password` (String, Optional, Sensitive) Pre-shared key. The device stores it
  as a token-keyed envelope and returns it on read; leave it unset to keep the
  stored value (the provider recovers the key from the read envelope and
  re-encodes it with the live session token), or set it to replace it.
- `active` (Boolean, Optional) Whether the SSID is broadcast.
- `security_type` (String, Optional) Security mode, e.g. `WPA2-PSK`,
  `WPA3-SAE`, `WPA3-SAE/WPA2-PSK`.
- `encryption` (String, Optional) Personal encryption cipher, e.g. `AES`.
- `group_key` (Number, Optional) Group key rekey interval in seconds.
- `mfp` (String, Optional) Management frame protection: `capable`, `required` or
  `disabled`.
- `hidden_ssid` (Boolean, Optional) Whether the SSID is hidden.
- `internet_only` (Boolean, Optional) Restrict clients to internet access only.
- `ap_isolate` (Boolean, Optional) Isolate wireless clients from each other
  (AP isolation).
- `web_ui_access` (Boolean, Optional) Allow clients to reach the device web UI.
- `wmf` (Boolean, Optional) Wireless multicast forwarding.
- `ft` (Boolean, Optional) 802.11r fast transition.
- `access_control` (Boolean, Optional) Whether MAC access control is enabled.
- `client_limit` (Number, Optional) Maximum number of clients allowed on the
  SSID (the device's `numClient.set`).

Fields that are not set are left at their current device value.

## Management notes

- Only the attributes you set are written; everything else keeps its current
  value, so an SSID can be managed partially (for example only its `name`).
- `band` and `index` select the slot. Changing either forces a new resource and
  **disables** the previous one.
- Destroying the resource **disables** the SSID; slots cannot be removed.
- `password` is stored in Terraform state, and changes made to it outside
  Terraform are not detected.

## Attributes Reference

In addition to the arguments above:

- `mac_address` (String) BSSID of the SSID.
- `ssid_type` (String) Device classification of the slot (`primary` or `guest`).

## Import

Import using `<band>/<index>`:

```sh
terraform import synclayer_sxep200w_wifi_ssid.primary_5g 5g/0
```

Only the primary (`0`) and secondary (`1`) slots can be imported — the device
web UI does not expose any others (some firmware revisions report extra slots
such as `5g/2`).

If such a slot was imported before this restriction, remove its resource block
from the configuration and run `terraform plan`/`apply`: it can no longer be
validated (or refreshed) while the block declares `index >= 2`, but once the
block is gone it is read and deleted, which disables that SSID on the device.
