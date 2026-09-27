resource "synclayer_sxep200w_wifi_global" "main" {
  wps_active = false

  # Mesh is off; this device is not part of a mesh network.
  mesh_mode = 0
}
