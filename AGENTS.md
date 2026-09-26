# AGENTS.md

Guidance for AI coding agents and automated reviewers working in this
repository. It is also used as the CodeRabbit knowledge base
(`.coderabbit.yaml`).

## Project

An unofficial Terraform provider for **SyncLayer** home gateways, built with
`terraform-plugin-sdk/v2`. It drives the device's local web management REST API
(`/api/v1/...`).

## Naming convention

- The **provider** is scoped to the manufacturer: `synclayer`.
- **Resources and data sources** are namespaced by product. The `SXEP200W`
  gateway uses:
  - `synclayer_sxep200w_port_forwarding`
  - `synclayer_sxep200w_static_route`
  - `synclayer_sxep200w_reserved_ip`
  - `synclayer_sxep200w_ddns` (singleton)
  - `synclayer_sxep200w_dmz` (singleton)
  - data source `synclayer_sxep200w_system`

When adding support for another product, follow the same pattern:
`synclayer_<product>_<resource>`.

## Layout

```
main.go                     plugin entrypoint
internal/provider/          provider schema, resources, data sources
internal/synclayer/         device REST client, crypto, wire types
docs/                       Terraform Registry docs (hand-written)
examples/                   runnable examples (tfplugindocs layout)
flake.nix                   dev shell (go, terraform, gopls)
```

## Build and test

Use the Nix dev shell (`.envrc` runs `use flake`):

```sh
nix develop
make build          # ./terraform-provider-synclayer
make test           # go test ./...
go vet ./...
gofmt -l .
```

## Invariants

- **Never commit credentials or device identifiers.** Test vectors in
  `internal/synclayer/crypto_test.go` are synthetic and generated
  independently; keep them that way. `gitleaks` is enabled.
- The management API is **plain HTTP by design**. Do not report the missing TLS
  as a vulnerability; do report leaked secrets or tokens.
- Preserve **plan/apply idempotency**. Be careful with `Optional` +
  `Computed` attributes and use `CustomizeDiff` where a value is recomputed.
- Tests must not require a live device; use `httptest` fakes.

## Device API notes

- Auth: `GET /api/v1/gateway/users/login/auth` returns a `web_key`;
  `POST /api/v1/gateway/users/login` takes
  `base64("HS\x0e" + user + "\x0e" + hex(PBKDF2-HMAC-SHA512(password, web_key, 2048, 32)))`.
  The returned `accessToken` is sent as the `Access-Token` header.
- The DDNS password uses AES-128-CBC (zero padding, fixed IV, key = first 16
  chars of the access token).
- The WiFi PSK uses the same token-keyed envelope. `GET /wifi/{band}/ssid` and
  `GET /wifi/{band}/ssid/{index}` return it in that form, and `PUT` keeps the
  envelope it is given. Verified on VER-01.06.05-EA: replaying the read envelope
  preserves the PSK, while **omitting** the password field replaces it, so the
  password must always be sent. Because the envelope is bound to the session
  token, `GetWifiSSID` recovers the plaintext with the token that produced it so
  an update can re-encode it with the token in use (a re-authentication between
  the read and the write otherwise invalidates the envelope).
- `GET /wifi/{band}/radio` answers **202** with a body, and includes a
  `channelList` and `used` fields under `basic`; a `PUT` without them is accepted
  (verified), so they are intentionally not carried through.
- Some operations have firmware quirks: static route `PUT` deletes and
  re-creates the rule (a new id is assigned), port forwarding deletes via `POST`,
  and the device may override the DDNS `url` for built-in providers. The client
  and resources account for these.
