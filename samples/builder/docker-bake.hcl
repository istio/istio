variable "HUB" {
  default = "localhost:5000"
}

variable "PLATFORMS" {
  default = "linux/amd64,linux/arm64"
}

images = [
  {
    name   = "tcp-echo-server"
    source = "../tcp-echo/src"
    tags   = ["1.3", "latest"]
  },
  {
    name   = "examples-helloworld-v1"
    source = "../helloworld/src"
    args   = {
      service_version = "v1"
    }
    tags = ["1.0", "latest"]
  },
  {
    name   = "examples-helloworld-v2"
    source = "../helloworld/src"
    args   = {
      service_version = "v2"
    }
    tags = ["1.0", "latest"]
  },
  {
  name = "mtls-server"
  source = "../mtls-server/src" 
  tags = ["1.0", "latest"]
  },
]

target "default" {
  matrix = {
    item = images
  }
  name    = item.name
  context = "${item.source}"
  tags    = [
    for x in setproduct([HUB], item.tags) : join("/${item.name}:", x)
  ]
  args      = lookup(item, "args", {})
  platforms = split(",", lookup(item, "platforms", PLATFORMS))
}
