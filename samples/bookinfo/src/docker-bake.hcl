variable "TAG" {
  default = "latest"
}

variable "HUB" {
  default = "localhost:5000"
}

// TODO: in buildx 0.11 they have matrix build which will help this

// Productpage
target "examples-bookinfo-productpage-v1" {
  tags = ["${HUB}/examples-bookinfo-productpage-v1:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  context = "./samples/bookinfo/src/productpage"
}
target "examples-bookinfo-productpage-v-flooding" {
  tags = ["${HUB}/examples-bookinfo-productpage-v-flooding:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    flood_factor = 100
  }
  context = "./samples/bookinfo/src/productpage"
}

// Details
target "examples-bookinfo-details-v1" {
  tags = ["${HUB}/examples-bookinfo-details-v1:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v1"
  }
  context = "./samples/bookinfo/src/details"
}
target "examples-bookinfo-details-v2" {
  tags = ["${HUB}/examples-bookinfo-details-v2:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v2"
    enable_external_book_service = true
  }
  context = "./samples/bookinfo/src/details"
}

// Reviews
target "examples-bookinfo-reviews-v1" {
  tags = ["${HUB}/examples-bookinfo-reviews-v1:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v1"
  }
  context = "./samples/bookinfo/src/reviews"
}
target "examples-bookinfo-reviews-v2" {
  tags = ["${HUB}/examples-bookinfo-reviews-v2:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v2"
    enable_ratings = true
  }
  context = "./samples/bookinfo/src/reviews"
}
target "examples-bookinfo-reviews-v3" {
  tags = ["${HUB}/examples-bookinfo-reviews-v3:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v3"
    enable_ratings = true
    star_color = "red"
  }
  context = "./samples/bookinfo/src/reviews"
}

// Ratings
target "examples-bookinfo-ratings-v1" {
  tags = ["${HUB}/examples-bookinfo-ratings-v1:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v1"
  }
  context = "./samples/bookinfo/src/ratings"
}
target "examples-bookinfo-ratings-v2" {
  tags = ["${HUB}/examples-bookinfo-ratings-v2:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v2"
  }
  context = "./samples/bookinfo/src/ratings"
}
target "examples-bookinfo-ratings-v-faulty" {
  tags = ["${HUB}/examples-bookinfo-ratings-v-faulty:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v-faulty"
  }
  context = "./samples/bookinfo/src/ratings"
}
target "examples-bookinfo-ratings-v-delayed" {
  tags = ["${HUB}/examples-bookinfo-ratings-v-delayed:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v-delayed"
  }
  context = "./samples/bookinfo/src/ratings"
}
target "examples-bookinfo-ratings-v-unavailable" {
  tags = ["${HUB}/examples-bookinfo-ratings-v-unavailable:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v-unavailable"
  }
  context = "./samples/bookinfo/src/ratings"
}
target "examples-bookinfo-ratings-v-unhealthy" {
  tags = ["${HUB}/examples-bookinfo-ratings-v-unhealthy:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  args = {
    service_version = "v-unhealthy"
  }
  context = "./samples/bookinfo/src/ratings"
}

// mysql
target "examples-bookinfo-mysqldb" {
  tags = ["${HUB}/examples-bookinfo-mysqldb:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  context = "./samples/bookinfo/src/mysql"
}

// mongo
target "examples-bookinfo-mongodb" {
  tags = ["${HUB}/examples-bookinfo-mongodb:${TAG}"]
  platforms = ["linux/amd64", "linux/arm64"]
  context = "./samples/bookinfo/src/mongodb"
}


// All targets
group "default" {
  targets = [
    "examples-bookinfo-productpage-v1",
    "examples-bookinfo-productpage-v-flooding",
    "examples-bookinfo-details-v1",
    "examples-bookinfo-details-v2",
    "examples-bookinfo-reviews-v1",
    "examples-bookinfo-reviews-v2",
    "examples-bookinfo-reviews-v3",
    "examples-bookinfo-ratings-v1",
    "examples-bookinfo-ratings-v2",
    "examples-bookinfo-ratings-v-faulty",
    "examples-bookinfo-ratings-v-delayed",
    "examples-bookinfo-ratings-v-unavailable",
    "examples-bookinfo-ratings-v-unhealthy",
    "examples-bookinfo-mysqldb",
    "examples-bookinfo-mongodb",
  ]
}
