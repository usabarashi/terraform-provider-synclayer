resource "synclayer_sxep200w_port_triggering" "game" {
  description = "game console"

  triggered {
    protocol    = "tcp"
    start_range = 59998
    end_range   = 59998
  }

  forwarded {
    protocol    = "tcp"
    start_range = 59997
    end_range   = 59997
  }
}
