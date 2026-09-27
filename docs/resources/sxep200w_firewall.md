---
page_title: "synclayer_sxep200w_firewall Resource"
description: |-
  Manages the firewall of the SXEP200W.
---

# synclayer_sxep200w_firewall

Manages the device firewall.

The device reports the same settings through an aggregate document and through
per-family endpoints, so this resource is the single owner of both. Its levels
select banks of preset rules whose contents are read-only and are not exposed
here. Destroying this resource removes it from state and **leaves the firewall
as it is**: switching protection off is not a neutral default.

## Example Usage

```terraform
resource "synclayer_sxep200w_firewall" "main" {
  ipv4_active = true
  ipv4_level  = "low"

  ipv4_block_ip_flood            = true
  ipv4_block_port_scan_detection = false

  ipv6_active = true
}
```

## Argument Reference

- `ipv4_active` (Boolean, Optional) Whether the IPv4 firewall is enabled.
- `ipv4_level` (String, Optional) IPv4 firewall level, which selects a bank of
  preset rules: `low`, `medium` or `high`.
- `ipv4_block_fragmented_ip_packets` (Boolean, Optional) Drop fragmented IPv4
  packets.
- `ipv4_block_port_scan_detection` (Boolean, Optional) Detect and drop IPv4
  port scans.
- `ipv4_block_ip_flood` (Boolean, Optional) Drop IPv4 flood traffic.
- `ipv4_block_ip_spoofing` (Boolean, Optional) Drop IPv4 packets with a spoofed
  source address.
- `ipv6_active` (Boolean, Optional) Whether the IPv6 firewall is enabled.

Fields that are not set are left at their current device value.

## Management notes

- Changing `ipv4_level` selects a different rule bank; the block flags are kept
  and are written independently of it.
- The rules inside a level are preset by the device and are not managed here.
- Destroying the resource leaves the firewall running as it is; remove the block
  to stop managing it.

## Import

Import using the fixed id `firewall`:

```sh
terraform import synclayer_sxep200w_firewall.main firewall
```
