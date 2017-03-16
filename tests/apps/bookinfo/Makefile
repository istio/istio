# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

build.productpage:
	@GOOS=linux GOARCH=amd64 go build -o productpage productpage.go

build.reviews:
	@GOOS=linux GOARCH=amd64 go build -o reviews reviews.go

build.ratings:
	@GOOS=linux GOARCH=amd64 go build -o ratings ratings.go

build.details:
	@GOOS=linux GOARCH=amd64 go build -o details details.go

dockerize.reviews: dockerize-reviews-v1 dockerize-reviews-v2 dockerize-reviews-v3

dockerize-reviews-v1:
	@docker build -f Dockerfile.reviews -t go-bookinfo-reviews-v1:latest .
dockerize-reviews-v2:
	@docker build -f Dockerfile.reviews -t go-bookinfo-reviews-v2:latest --build-arg enable_ratings=true .
dockerize-reviews-v3:
	@docker build -f Dockerfile.reviews -t go-bookinfo-reviews-v3:latest --build-arg enable_ratings=true --build-arg star_color=red .
dockerize-productpage:
	@docker build -f Dockerfile.productpage -t go-bookinfo-productpage-v1:latest .
dockerize-ratings:
	@docker build -f Dockerfile.ratings -t go-bookinfo-ratings-v1:latest .
dockerize-details:
	@docker build -f Dockerfile.details -t go-bookinfo-details-v1:latest .
clean:
	@rm productpage ratings details reviews
