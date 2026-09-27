resource "synclayer_sxep200w_lan" "main" {
  dhcp_active     = true
  dhcp_start_ip   = "192.168.0.100"
  dhcp_end_ip     = "192.168.0.149"
  dhcp_lease_time = 86400
}
