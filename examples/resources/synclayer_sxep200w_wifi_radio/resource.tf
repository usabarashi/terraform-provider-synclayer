resource "synclayer_sxep200w_wifi_radio" "five_g" {
  band         = "5g"
  active       = true
  channel      = "auto"
  bandwidth    = "80"
  output_power = "high"
}
