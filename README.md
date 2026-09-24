# terraform-provider-synclayer

An unofficial Terraform provider for **SyncLayer** home gateways. The provider
is scoped to the manufacturer, while resources and data sources are namespaced
by product (`sxep200w` for the SXEP200W GPON gateway). It drives the device's
local web management REST API (`/api/v1/...`) so that port forwarding, static
routes, DHCP reservations, DDNS and DMZ can be managed as code.

> The provider authenticates against the same endpoint the web UI uses. Field
> names and behaviours may differ between firmware revisions.

## Requirements

* Terraform >= 1.0
* Go >= 1.26 (only to build the provider)
* Network access to the device management interface

## Building

```sh
make build            # produces ./terraform-provider-synclayer
make install          # installs it into ~/.terraform.d/plugins
make test             # unit tests
```

A Nix flake is provided for a reproducible toolchain:

```sh
nix develop
```

## Provider configuration

```hcl
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
  password = var.device_password
}
```

All arguments can be supplied through environment variables:

| Argument   | Environment variable  | Default                 |
| ---------- | --------------------- | ----------------------- |
| `host`     | `SYNCLAYER_HOST`      | *(required)*            |
| `username` | `SYNCLAYER_USERNAME`  | `admin`                 |
| `password` | `SYNCLAYER_PASSWORD`  | *(required)*            |
| `insecure` | –                     | `false`                 |
| `timeout`  | –                     | `30` (seconds)          |

> **Security note:** the web UI obfuscates credentials before sending them, but
> the transport itself is plain HTTP. Only use this provider on a trusted LAN.

## Resources

All resources are namespaced by product; the `SXEP200W` resources are:

| Resource                              | Description                             |
| ------------------------------------- | --------------------------------------- |
| `synclayer_sxep200w_port_forwarding`  | Port forwarding (NAPT) rule             |
| `synclayer_sxep200w_static_route`     | IPv4 static route                       |
| `synclayer_sxep200w_reserved_ip`      | DHCP reserved IP address (static lease) |
| `synclayer_sxep200w_ddns`             | Dynamic DNS client (singleton)          |
| `synclayer_sxep200w_dmz`              | DMZ host (singleton)                    |

## Data sources

| Data source                  | Description                              |
| ---------------------------- | ---------------------------------------- |
| `synclayer_sxep200w_system`  | Model, serial number and firmware version |

## Example

```hcl
data "synclayer_sxep200w_system" "this" {}

resource "synclayer_sxep200w_port_forwarding" "ssh" {
  service_type        = "SSH"
  ip_address          = "192.168.0.10"
  protocol            = "tcp"
  local_port_start    = 22
  local_port_end      = 22
  external_port_start = 2222
  external_port_end   = 2222
}

resource "synclayer_sxep200w_reserved_ip" "nas" {
  mac_address = "AA:BB:CC:DD:EE:FF"
  ip_address  = "192.168.0.20"
  device_name = "nas"
}

output "firmware" {
  value = data.synclayer_sxep200w_system.this.software_version
}
```

More examples live in [`examples/`](./examples) and per-resource documentation
in [`docs/`](./docs).

## A note on the API

Authentication is a two step challenge/response:

1. `GET /api/v1/gateway/users/login/auth` returns a per-session `web_key`.
2. `POST /api/v1/gateway/users/login` accepts a `password` derived as
   `base64("HS\x0e" + user + "\x0e" + hex(PBKDF2-HMAC-SHA512(password, web_key, 2048, 32)))`.

The returned `accessToken` is then sent as the `Access-Token` header on every
request. The provider re-authenticates transparently when the token expires.
The DDNS password uses a separate AES-128-CBC envelope keyed with the first 16
characters of the access token; both schemes are reproduced in
`internal/synclayer/crypto.go`.

## License

MIT
