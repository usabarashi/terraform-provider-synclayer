resource "synclayer_sxep200w_datetime" "main" {
  ntp_active  = true
  ntp_servers = ["ntp.nict.jp"]
  time_zone   = 127
}
