# Deployment Architecture

This document describes how `terraform-provider-synclayer` is distributed via the
HCP Terraform Private Registry and executed against a LAN-connected gateway.

> Status: **pre-1.0 / experimental.** The repository ships the release pipeline
> (`.goreleaser.yml`, `.github/workflows/release.yml`); the HCP Terraform
> registration, GitHub secrets, and the LAN execution environment are operated
> outside this repo. Interfaces may change without notice and the provider has not
> yet been validated against many firmware revisions.

Publishing requires a GPG key registered with your HCP Terraform organization and
an API token that can manage providers. Running the provider against a device
requires an execution environment **inside the device's LAN**.

## Overview

1. **GitHub Repository** — GitHub Actions builds multi-platform binaries with
   GoReleaser and uploads GPG-signed artifacts to the HCP Terraform Private
   Registry on every `v*` tag.
2. **HCP Terraform** — Dispatches Runs to the Agent Pool associated with the
   Workspace and serves provider binaries from the Private Registry.
3. **Agent (inside the LAN)** — Pulls Runs, downloads the provider from the Private
   Registry, and executes `terraform plan/apply` where the gateway is reachable.
4. **SyncLayer SXEP200W GPON gateway** — Resides on the same LAN as the agent. The
   provider talks to the device's local web management REST API (`/api/v1/...`),
   the same JSON API the device web UI uses.

## 1. Private Registry

### Why Private Registry

The gateway exposes no public/vendor API for external use — the provider drives the
device's local management API on a LAN-connected device. Publishing to the public
Terraform Registry is inappropriate because:

- The device requires direct LAN access (not reachable from the internet).
- The provider targets a specific hardware model (SyncLayer SXEP200W).

The HCP Terraform Private Registry distributes the provider within the organization
without running a custom registry server, and its Free tier includes the Private
Registry at no cost.

### Provider Source

With the Private Registry, the `source` changes from the local development form:

```hcl
# Local development (dev_overrides)
terraform {
  required_providers {
    synclayer = { source = "usabarashi/synclayer" }
  }
}

# Private Registry
terraform {
  required_version = ">= 1.5"

  required_providers {
    synclayer = {
      source  = "app.terraform.io/usabarashi/synclayer"
      version = "~> 0.1.0"
    }
  }
}
```

Pinning the `version` constraint prevents unintended upgrades, which matters while
the provider is pre-1.0. `~> 0.1.0` allows only `0.1.x`.

### Build Pipeline

The release pipeline ([`.github/workflows/release.yml`](../.github/workflows/release.yml))
runs on a pushed `v*` tag, first runs the test workflow, then produces signed
multi-platform binaries that HCP Terraform can consume.

> Use only stable `vX.Y.Z` tags. The push action parses checksum entries with a
> strict `X.Y.Z` version regex, so a prerelease tag (e.g. `v0.1.0-rc1`) would build
> GoReleaser artifacts but fail to upload to the registry.

**GoReleaser** ([`.goreleaser.yml`](../.goreleaser.yml)) generates:

- Multi-platform binaries (`linux_amd64`, `linux_arm64`, `darwin_amd64`,
  `darwin_arm64`)
- a `SHA256SUMS` file
- a `SHA256SUMS.sig` (GPG detached signature)

The archive name template follows the registry convention
`terraform-provider-synclayer_{{ .Version }}_{{ .Os }}_{{ .Arch }}.zip`, and the
binary inside is named `terraform-provider-synclayer_v{{ .Version }}`.

**Protocol version** — this provider is built on `terraform-plugin-sdk/v2`, which
serves the plugin protocol **5.0**. The workflow sets
`TF_PROVIDER_PLATFORMS: "5.0"` explicitly, because the push action defaults to `6.0`
(the protocol used by framework-based providers). If this is omitted, the registry
records the wrong protocol and `terraform init`/runs fail with a protocol mismatch.

**GPG signing** is required by the Private Registry. Register the **public** key of
your signing key with the organization, and pass its fingerprint and key id to the
workflow via secrets. (To export the public key: `gpg --armor --export <KEY_ID>`.)

**GitHub Actions** automates the build and upload via
`dcarbone/tfcloud-provider-push-action` with `TF_PROVIDER_NAME: synclayer`.

### Required GitHub Secrets

The release workflow expects these repository secrets:

| Secret | Purpose |
|--------|---------|
| `GPG_PRIVATE_KEY` | ASCII-armored private key imported before signing |
| `GPG_FINGERPRINT` | Fingerprint GoReleaser signs with |
| `GPG_KEY_ID` | Key id the Private Registry verifies against |
| `TFC_TOKEN` | HCP Terraform API token with registry write access |

```sh
R=usabarashi/terraform-provider-synclayer
gh secret set GPG_PRIVATE_KEY -R $R < /path/to/private-key.asc
gh secret set GPG_FINGERPRINT -R $R -b '<fingerprint>'
gh secret set GPG_KEY_ID       -R $R -b '<key id>'
gh secret set TFC_TOKEN        -R $R -b '<HCP Terraform API token>'
```

### One-time HCP Terraform Registration

Before the first tag, make sure the Private Registry has a target for the push
action:

1. In HCP Terraform (Organization Settings → Providers / Private Registry), add a
   provider named `synclayer` under your namespace (the push action can also create
   it on first upload).
2. Register the **public** key of your GPG signing key with the organization; the
   `GPG_KEY_ID` must match it.
3. Push a tag (`git tag v0.1.0 && git push origin v0.1.0`) to trigger the release.

## 2. Agent Mode

The gateway is on a private LAN with no inbound connectivity from the internet, so
HCP Terraform's default remote execution cannot reach it. Runs must therefore execute
on a machine **inside the LAN**. Use an HCP Terraform **agent pool** (workspace
execution mode "Agent") with an agent running on such a machine, or any equivalent
local execution environment.

The agent only needs:

- outbound HTTPS to HCP Terraform
- outbound HTTP/HTTPS to the device on the LAN

### HCP Terraform Workspace Setup

1. Create a **Workspace** with execution mode "Agent" and select an agent pool whose
   agents run inside the LAN.
2. Configure workspace variables for provider configuration: `synclayer_host`
   (`http://<device-ip>`), `synclayer_username`, `synclayer_password` (sensitive).
3. Terraform state is stored and managed by HCP Terraform; the agent retains no
   state locally.

## End-to-End Flow

```
1. Developer pushes a tag (e.g. v0.1.0) to GitHub
2. GitHub Actions:
   a. Runs the test workflow
   b. GoReleaser builds binaries for all platforms
   c. GPG signs SHA256SUMS
   d. tfcloud-provider-push-action uploads to the HCP Terraform Private Registry
3. Developer (or VCS-driven trigger) queues a Run in the synclayer Workspace
4. HCP Terraform dispatches the Run to the Agent Pool
5. Agent (inside the LAN):
   a. Pulls the Run
   b. Downloads terraform-provider-synclayer from the Private Registry
   c. Executes terraform init / plan / apply
   d. Provider connects to the gateway over HTTP on the LAN
6. Run results are reported back to HCP Terraform
```

### Security Consideration

The provider communicates with the gateway over plaintext HTTP. This is a
limitation of the SXEP200W firmware (its management interface does not serve TLS).
Credentials are obfuscated before transport (PBKDF2 challenge/response for login and
an AES envelope for the DDNS password), but the transport itself is unencrypted on
the LAN. Mitigate by keeping management traffic on a trusted LAN segment not exposed
to untrusted hosts. The client also confines HTTP redirects to the device origin so
the session token is not forwarded elsewhere.
