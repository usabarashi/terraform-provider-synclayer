resource "synclayer_sxep200w_static_route_ipv6" "lan" {
  destination_ip = "2001:db8:1::"
  prefix_length  = 64
  gateway        = "fe80::1"
}

resource "synclayer_sxep200w_static_route_ipv6" "wan" {
  destination_ip = "2001:db8:2::"
  prefix_length  = 48
  gateway        = "fe80::2"
  interface      = "wan"
  interface_name = "veip0.1"
}
