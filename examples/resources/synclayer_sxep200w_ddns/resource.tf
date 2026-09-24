resource "synclayer_sxep200w_ddns" "dyndns" {
  active        = true
  provider_name = "DynDNS"
  username      = "example"
  password      = var.ddns_password
  hostname      = "example.dyndns.org"
}

variable "ddns_password" {
  type      = string
  sensitive = true
}

output "ddns_endpoint" {
  value = synclayer_sxep200w_ddns.dyndns.resolved_url
}
