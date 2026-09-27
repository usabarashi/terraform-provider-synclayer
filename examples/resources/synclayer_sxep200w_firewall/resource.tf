resource "synclayer_sxep200w_firewall" "main" {
  ipv4_active = true
  ipv4_level  = "low"

  ipv4_block_ip_flood            = true
  ipv4_block_port_scan_detection = false

  ipv6_active = true
}
