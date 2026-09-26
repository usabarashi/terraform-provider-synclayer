resource "synclayer_sxep200w_wifi_ssid" "primary_5g" {
  band          = "5g"
  index         = 0
  active        = true
  name          = "home-5g"
  security_type = "WPA3-SAE/WPA2-PSK"
  password      = var.wifi_password
}

variable "wifi_password" {
  type      = string
  sensitive = true
}
