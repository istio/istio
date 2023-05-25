variable "TAG" {
  default = "latest"
}

variable "HUB" {
  default = "localhost:5000"
}

target "examples-bookinfo-productpage-v1" {
  tags = ["${HUB}/examples-bookinfo-productpage-v1:${TAG}"]
  context = "./samples/bookinfo/src/productpage"
}

target "examples-bookinfo-productpage-v-flooding" {
  tags = ["${HUB}/examples-bookinfo-productpage-v-flooding:${TAG}"]
  args = {
    flood_factor = 100
  }
  context = "./samples/bookinfo/src/productpage"
}


group "default" {
  targets = [
    "examples-bookinfo-productpage-v1",
    "examples-bookinfo-productpage-v-flooding",
  ]
}
