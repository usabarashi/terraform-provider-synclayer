resource "synclayer_sxep200w_network_options" "main" {
  wan_icmpv4_blocking = true
  wan_icmpv6_blocking = true

  ipsec_passthrough = false
  pptp_passthrough  = false
  l2tp_passthrough  = false

  nat_tcp_timer = 3600
  nat_udp_timer = 300

  multicast = false

  # The complete set of enabled helpers: anything this list leaves out is
  # switched off.
  alg_enabled = ["ftp", "sip"]
}
