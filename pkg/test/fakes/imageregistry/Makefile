# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: build build_and_push clean all

MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
MD_PATH := $(dir $(MKFILE_PATH))
HUB ?= gcr.io/istio-testing
IMG := $(HUB)/fake-registry
BIN_NAME := main

# NOTE: TAG should be updated whenever changes are made in this directory
# This should also be updated in dependent components
TAG := 1.3

all: build_and_push clean

$(MD_PATH)/$(BIN_NAME)-amd64: $(MD_PATH)/main.go
	cd $(MD_PATH) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BIN_NAME)-amd64 -a -tags netgo -ldflags '-w -extldflags "-static"' main.go

$(MD_PATH)/$(BIN_NAME)-arm64: $(MD_PATH)/main.go
	cd $(MD_PATH) && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(BIN_NAME)-arm64 -a -tags netgo -ldflags '-w -extldflags "-static"' main.go

build: $(MD_PATH)/$(BIN_NAME)-amd64  $(MD_PATH)/$(BIN_NAME)-arm64

build_and_push: build
	docker buildx build --platform=linux/amd64,linux/arm64 $(MD_PATH) -t $(IMG):$(TAG) -t $(IMG):latest --push

clean:
	rm $(MD_PATH)/$(BIN_NAME)-*
