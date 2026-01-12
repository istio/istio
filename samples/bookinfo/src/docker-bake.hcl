variable "TAGS" {
  default = "latest"
}

variable "HUB" {
  default = "localhost:5000"
}

variable "PLATFORMS" {
  default = "linux/amd64,linux/arm64"
}

images = [
  // Productpage
  {
    name   = "examples-bookinfo-productpage-v1"
    source = "productpage"
  },
  {
    name = "examples-bookinfo-productpage-v-flooding"
    args = {
      flood_factor = 100
    }
    source = "productpage"
  },

  // Details
  {
    name = "examples-bookinfo-details-v1"
    args = {
      service_version = "v1"
    }
    source = "details"
  },
  {
    name = "examples-bookinfo-details-v2"
    args = {
      service_version              = "v2"
      enable_external_book_service = true
    }
    source = "details"
  },

  // Reviews
  {
    name = "examples-bookinfo-reviews-v1"
    args = {
      service_version = "v1"
    }
    source = "reviews"
  },
  {
    name = "examples-bookinfo-reviews-v2"
    args = {
      service_version = "v2"
      enable_ratings  = true
    }
    source = "reviews"
  },
  {
    name = "examples-bookinfo-reviews-v3"
    args = {
      service_version = "v3"
      enable_ratings  = true
      star_color      = "red"
    }
    source = "reviews"
  },

  // Ratings
  {
    name = "examples-bookinfo-ratings-v1"
    args = {
      service_version = "v1"
    }
    source = "ratings"
  },
  {
    name = "examples-bookinfo-ratings-v2"
    args = {
      service_version = "v2"
    }
    source = "ratings"
  },
  {
    name = "examples-bookinfo-ratings-v-faulty"
    args = {
      service_version = "v-faulty"
    }
    source = "ratings"
  },
  {
    name = "examples-bookinfo-ratings-v-delayed"
    args = {
      service_version = "v-delayed"
    }
    source = "ratings"
  },
  {
    name = "examples-bookinfo-ratings-v-unavailable"
    args = {
      service_version = "v-unavailable"
    }
    source = "ratings"
  },
  {
    name = "examples-bookinfo-ratings-v-unhealthy"
    args = {
      service_version = "v-unhealthy"
    }
    source = "ratings"
  },

  // mysql
  {
    name   = "examples-bookinfo-mysqldb"
    source = "mysql"
  },

  // mongo
  {
    name   = "examples-bookinfo-mongodb"
    source = "mongodb"
  }
]

target "default" {
  matrix = {
    item = images
  }
  name    = item.name
  context = "./samples/bookinfo/src/${item.source}"
  tags    = [
    for x in setproduct([HUB], "${split(",", TAGS)}") : join("/${item.name}:", x)
  ]
  args = lookup(item, "args", {})
  platforms = split(",",lookup(item, "platforms", PLATFORMS))
}
