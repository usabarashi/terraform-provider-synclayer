---
page_title: "synclayer_sxep200w_lan Resource"
description: |-
  Manages the LAN settings of the SXEP200W: its DHCP server.
---

# synclayer_sxep200w_lan

Manages the LAN settings of the device: its DHCP server.

The LAN address, the subnet mask and the interface MAC address are **read-only**
here. They are read into state so that the values the device is using are
visible and can be referenced, but they are not arguments: changing the address
from Terraform would move the address the provider — and the rest of the
network — reaches the device on, leaving the apply unable to finish the rest of
its work. **Change them in the device web UI, where the change is made
deliberately and where the rest of the network can be updated with it.**

Destroying this resource removes it from state and leaves the device as it is.

## Example Usage

```terraform
resource "synclayer_sxep200w_lan" "main" {
  dhcp_active     = true
  dhcp_start_ip   = "192.168.0.100"
  dhcp_end_ip     = "192.168.0.149"
  dhcp_lease_time = 86400
}
```

## Argument Reference

- `dhcp_active` (Boolean, Optional) Whether the DHCP server is enabled.
- `dhcp_start_ip` (String, Optional) First address of the DHCP pool.
- `dhcp_end_ip` (String, Optional) Last address of the DHCP pool.
- `dhcp_lease_time` (Number, Optional) Lease time in seconds. The web UI shows
  hours.
- `dhcp_wins_server` (String, Optional) WINS server handed to DHCP clients.
- `dhcp_assignment` (String, Optional) How addresses are assigned (`manual`
  selects from the registered addresses).

Fields that are not set are left at their current device value.

## Attributes Reference

In addition to the arguments above, the following are read-only:

- `ip_address` (String) LAN address of the device.
- `subnet` (String) LAN subnet mask.
- `mac_address` (String) MAC address of the LAN interface.

These three cannot be set here. Set them in the device web UI (see
[Management notes](#management-notes)); a configuration that assigns one of them
is refused rather than silently ignored.

## Management notes

- **The LAN address and subnet are managed by hand.** The provider reads them so
  that drift is visible in state, but it never writes them. Changing the address
  from Terraform would cut the connection the apply itself is using: the
  provider talks to a fixed host, and the resources after that point would fail
  with the change half applied. Doing it in the web UI lets the change be made
  together with everything that depends on it.
- The device replaces the whole LAN object on every write, so the provider reads
  it first and changes only the DHCP fields in it. The IPv6 block the device
  reports is carried through untouched.
- A write of the DHCP fields leaves the address, the subnet and the pool range
  alone (verified on VER-01.06.05-EA: changing the lease time changed nothing
  else).
- The device offers no configuration backup on this firmware, so keep a note of
  the address and the DHCP range outside the device.
- Destroying the resource leaves the LAN as it is; remove the block to stop
  managing the DHCP settings.

## Import

Import using the fixed id `lan`:

```sh
terraform import synclayer_sxep200w_lan.main lan
```
