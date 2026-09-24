resource "synclayer_sxep200w_static_route" "lab" {
  destination_ip = "10.10.0.0"
  subnet         = "255.255.0.0"
  gateway        = "192.168.0.1"
  interface      = "wan"
  interface_name = "veip0.1"
  active         = true
}
