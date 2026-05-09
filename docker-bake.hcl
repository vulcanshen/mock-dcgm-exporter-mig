variable "REGISTRY" {
  default = ""
}

variable "TAG" {
  default = "latest"
}

variable "OUTPUT_DIR" {
  default = "./dist"
}

group "default" {
  targets = ["amd64", "arm64"]
}

target "amd64" {
  dockerfile = "Dockerfile"
  context    = "."
  platforms  = ["linux/amd64"]
  tags       = ["${REGISTRY}mock-dcgm-exporter-mig:${TAG}"]
  output     = ["type=docker,dest=${OUTPUT_DIR}/mock-dcgm-exporter-mig-amd64.tar"]
}

target "arm64" {
  dockerfile = "Dockerfile"
  context    = "."
  platforms  = ["linux/arm64"]
  tags       = ["${REGISTRY}mock-dcgm-exporter-mig:${TAG}"]
  output     = ["type=docker,dest=${OUTPUT_DIR}/mock-dcgm-exporter-mig-arm64.tar"]
}
