Add Terraform provider for SyncLayer SXEP200W gateways

## Why
SyncLayer's SXEP200W GPON gateway can only be configured through its local web
UI, so port forwarding, static routes, DHCP reservations, DDNS and DMZ settings
live outside version control and have to be maintained by hand. This adds a
Terraform provider so those settings can be managed declaratively, reviewed like
code, and reproduced on replacement hardware.

## What
- Drive the gateway's own JSON API (`/api/v1`) directly. The management UI is a
  client-side-rendered SPA, so that API is the stable contract and the same
  surface the vendor's own UI relies on; scraping the rendered DOM or automating
  the UI would have been brittle, slower, and harder to keep working across
  firmware revisions.
- Scope the provider to the manufacturer (`synclayer`) and namespace every
  resource and data source by product (`synclayer_sxep200w_*`). That keeps the
  provider address stable and leaves room for additional SyncLayer models
  without renaming existing resources.
- Build on `terraform-plugin-sdk/v2` rather than the newer plugin framework. The
  managed surface is small and CRUD-shaped, so the SDK keeps the schema and
  lifecycle code compact; the framework's additional type ceremony would not pay
  for itself here. Switching later remains possible since the client layer is
  framework-agnostic.
- Reimplement the device's login handshake (a PBKDF2-HMAC-SHA512 challenge /
  response plus the custom `Access-Token` header) with transparent
  re-authentication, and reproduce the AES envelope the UI applies to the DDNS
  password. Firmware quirks — static-route updates reassigning the rule id,
  port-forwarding deletion via `POST`, and built-in DDNS providers overriding the
  configured URL — are absorbed by the API client so the resources stay
  straightforward.
- Bias toward plan/apply correctness and credential hygiene: the configured DDNS
  URL is kept separate from the computed effective endpoint, `Optional` +
  `Computed` behaviour is made explicit where the device normalises values,
  redirects are confined to the device origin so the session token cannot leak,
  and tests use synthetic vectors plus `httptest` fakes so CI needs no physical
  device.

## References
N/A
