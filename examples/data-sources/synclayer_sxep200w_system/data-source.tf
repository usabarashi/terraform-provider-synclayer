data "synclayer_sxep200w_system" "this" {}

output "model" {
  value = data.synclayer_sxep200w_system.this.model_name
}

output "firmware_version" {
  value = data.synclayer_sxep200w_system.this.software_version
}
