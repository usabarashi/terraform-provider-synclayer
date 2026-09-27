resource "synclayer_sxep200w_eco_mode" "main" {
  active           = true
  schedule_enabled = true
  start_time       = "23:00"
  end_time         = "06:00"
}
