---
page_title: "synclayer_sxep200w_network_options Resource"
description: |-
  Manages the device-wide network options (Advanced > Network) and the ALG helpers on the SXEP200W.
---

# synclayer_sxep200w_network_options

Manages the device-wide network options the web UI groups under Advanced >
Network, together with the application-layer gateway (ALG) helpers that share
that page.

Each is a single value on the device, so they share one resource. Destroying
this resource removes it from state and **leaves the device as it is**: these
options have no neutral value — clearing a blocking flag loosens traffic, while
clearing a passthrough flag restricts it.

## Example Usage

```terraform
resource "synclayer_sxep200w_network_options" "main" {
  wan_icmpv4_blocking = true
  wan_icmpv6_blocking = true

  ipsec_passthrough = false
  pptp_passthrough  = false
  l2tp_passthrough  = false

  nat_tcp_timer = 3600
  nat_udp_timer = 300

  multicast = false

  # The complete set of enabled helpers: anything this list leaves out is
  # switched off.
  alg_enabled = ["ftp", "sip"]
}
```

## Argument Reference

- `wan_icmpv4_blocking` (Boolean, Optional) Block ICMPv4 (ping) from the WAN.
- `wan_icmpv6_blocking` (Boolean, Optional) Block ICMPv6 from the WAN.
- `ipsec_passthrough` (Boolean, Optional) Let IPsec traffic through to clients.
- `pptp_passthrough` (Boolean, Optional) Let PPTP traffic through to clients.
- `l2tp_passthrough` (Boolean, Optional) Let L2TP traffic through to clients.
- `nat_tcp_timer` (Number, Optional) TCP session timeout of the NAT/SPI table,
  in seconds.
- `nat_udp_timer` (Number, Optional) UDP session timeout of the NAT/SPI table,
  in seconds.
- `remote_access_enabled` (Boolean, Optional) Offer management from the WAN
  side. Enable deliberately: it exposes the device web UI beyond the LAN.
- `secure_access_enabled` (Boolean, Optional) Offer secure (HTTPS) management
  from the WAN side.
- `management_port` (Number, Optional) Port for WAN-side management.
- `multicast` (Boolean, Optional) Allow multicast through the device.
- `alg_enabled` (Set of String, Optional) Application-layer gateway helpers to
  leave enabled, by service code (`ftp`, `sip`, …).

Fields that are not set are left at their current device value.

## Management notes

- The device replaces the whole options object on every write and refuses a
  partial one, so the provider always reads it before changing it. A field that
  is not configured keeps whatever the device reports.
- `alg_enabled` is the **complete** set of enabled helpers: the device switches
  off any helper the list leaves out, so a helper that appears later shows up as
  a difference on the next plan rather than being silently kept. When the
  attribute is not configured at all, the helper list is not written.
- The device keeps **one** management port for both the remote and the secure
  service, so `management_port` sets both. Changing it restarts the device web
  service, which drops the connection that is writing it; the provider confirms
  the write with a fresh read before reporting a failure.
- `remote_access_enabled` and `secure_access_enabled` are separate, but the port
  they listen on is not.
- Destroying the resource leaves the device untouched; remove the block to stop
  managing these values, and change them elsewhere afterwards if you want them
  different.

## Import

Import using the fixed id `network_options`:

```sh
terraform import synclayer_sxep200w_network_options.main network_options
```
