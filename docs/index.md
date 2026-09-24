---
page_title: "synclayer Provider"
description: |-
  Manage SyncLayer home gateways.
---

# synclayer Provider

The `synclayer` provider manages the local web management API of SyncLayer home
gateways. The provider is scoped to the **manufacturer**; each resource and
data source is namespaced by **product**. For the `SXEP200W` gateway that means
resource types such as `synclayer_sxep200w_port_forwarding` and the
`synclayer_sxep200w_system` data source.

## Example Usage

```terraform
terraform {
  required_providers {
    synclayer = {
      source = "usabarashi/synclayer"
    }
  }
}

provider "synclayer" {
  host     = "http://192.168.0.1"
  username = "admin"
  password = var.synclayer_password
}
```

## Schema

### Optional

- `host` (String) Base URL of the device management interface, e.g.
  `http://192.168.0.1`. The scheme may be omitted. Can also be set with the
  `SYNCLAYER_HOST` environment variable. This argument is required.
- `username` (String) Login ID. Can also be set with the `SYNCLAYER_USERNAME`
  environment variable. Defaults to `admin`.
- `password` (String, Sensitive) Login password. Can also be set with the
  `SYNCLAYER_PASSWORD` environment variable.
- `insecure` (Boolean) Skip TLS certificate verification. Defaults to `false`.
- `timeout` (Number) HTTP request timeout in seconds. Defaults to `30`.
