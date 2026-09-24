resource "synclayer_sxep200w_port_forwarding" "ssh" {
  service_type        = "SSH"
  ip_address          = "192.168.0.10"
  protocol            = "tcp"
  local_port_start    = 22
  local_port_end      = 22
  external_port_start = 2222
  external_port_end   = 2222
  active              = true
}
