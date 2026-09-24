resource "synclayer_sxep200w_dmz" "game_console" {
  active      = true
  destination = "192.168.0.30"
  subnet      = "255.255.255.0"
}
