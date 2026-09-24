---
page_title: "synclayer_sxep200w_ddns Resource"
description: |-
  Manages the dynamic DNS (DDNS) client of the device.
---

# synclayer_sxep200w_ddns

Manages the dynamic DNS (DDNS) client of the device.

This is a **singleton** resource: there is only one DDNS configuration per
device. Destroying the resource disables DDNS rather than removing the
configuration.

## Example Usage

```terraform
resource "synclayer_sxep200w_ddns" "dyndns" {
  active        = true
  provider_name = "DynDNS"
  username      = "example"
  password      = var.ddns_password
  hostname      = "example.dyndns.org"
}
```

## Argument Reference

- `provider_name` (String, Required) Provider name as reported by the device:
  `DynDNS`, `NoIP` or `User Define`.
- `active` (Boolean, Optional) Whether the DDNS client is enabled. Defaults to
  `true`.
- `username` (String, Optional) Account username.
- `password` (String, Optional, Sensitive) Account password. **Write-only**: it
  is never read back from the device.
- `token` (String, Optional, Sensitive) Account token, when the provider uses
  token authentication.
- `hostname` (String, Optional) Hostname to register with the provider.
- `url` (String, Optional) Update URL to request. When omitted, the URL
  advertised by the selected provider is used. This value is authoritative for
  the `User Define` provider (out-of-band changes are detected and restored).
  The built-in DynDNS/NoIP providers ignore it and use their own endpoint;
  observe `resolved_url` for the effective value.

## Attributes Reference

In addition to the arguments above:

- `resolved_url` (String) Effective update URL in use on the device (the
  configured `url` or the provider default). This reflects the device's actual
  state, including out-of-band changes.
- `connection_status` (String) Last connection status reported by the device.
- `ip_address` (String) Last IP address reported by the device.

## Import

The singleton can be imported with the id `ddns`:

```sh
terraform import synclayer_sxep200w_ddns.dyndns ddns
```
