resource "synclayer_sxep200w_packet_filtering" "block_telnet" {
  family      = "ipv4"
  priority    = 20
  filter_type = "deny"
  protocol    = "tcp"
  description = "block telnet from the LAN"

  src {
    type = "any"
  }

  dest {
    type       = "any"
    start_port = 23
    end_port   = 23
  }
}
