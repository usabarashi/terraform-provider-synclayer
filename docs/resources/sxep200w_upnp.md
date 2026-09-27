---
page_title: "synclayer_sxep200w_upnp Resource"
description: |-
  Manages UPnP on the SXEP200W.
---

# synclayer_sxep200w_upnp

Manages UPnP on the device.

The port mappings UPnP creates are made by the clients that request them and are
not managed here. Destroying this resource turns UPnP off and keeps the interval
and the TTL.

## Example Usage

```terraform
resource "synclayer_sxep200w_upnp" "main" {
  active = false
}
```

## Argument Reference

- `active` (Boolean, Optional) Whether UPnP is enabled.
- `interval` (Number, Optional) Announcement interval in seconds.
- `ttl` (Number, Optional) Time to live, in hops, of UPnP announcements.

Fields that are not set are left at their current device value.

## Management notes

- The device requires all three fields on every write, so the provider reads the
  current values and replays the ones that are not configured.
- Switching UPnP off keeps `interval` and `ttl`; only `active` changes.
- The device rejects values outside its own ranges, which are narrower than the
  attribute types suggest (an interval of 15 seconds and a TTL of 1 are
  accepted; 11 seconds and 50 hops are not, on VER-01.06.05-EA).
- Destroying the resource turns UPnP off.

## Import

Import using the fixed id `upnp`:

```sh
terraform import synclayer_sxep200w_upnp.main upnp
```
