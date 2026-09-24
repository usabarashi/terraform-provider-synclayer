---
page_title: "synclayer_sxep200w_system Data Source"
description: |-
  Reads static information about the device.
---

# synclayer_sxep200w_system

Reads static information about the device (model, serial number, firmware
version).

## Example Usage

```terraform
data "synclayer_sxep200w_system" "this" {}

output "firmware_version" {
  value = data.synclayer_sxep200w_system.this.software_version
}
```

## Attributes Reference

- `id` (String) Serial number of the device.
- `model_name` (String) Device model, e.g. `SXEP200W`.
- `serial_no` (String) Device serial number.
- `hardware_version` (String) Hardware revision.
- `software_version` (String) Firmware version.
- `build_time` (String) Firmware build timestamp.
- `operation_time` (String) Uptime as reported by the device.
- `base_mac` (String) Base MAC address of the device.
