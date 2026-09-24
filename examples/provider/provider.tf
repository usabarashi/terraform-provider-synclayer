terraform {
  required_providers {
    synclayer = {
      source = "usabarashi/synclayer"
    }
  }
}

# The password can be supplied with the SYNCLAYER_PASSWORD environment variable
# instead of being written to disk.
provider "synclayer" {
  host     = "http://192.168.0.1"
  username = "admin"
  password = var.synclayer_password
}

variable "synclayer_password" {
  type      = string
  sensitive = true
}
