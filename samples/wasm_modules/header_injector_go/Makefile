# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY : all clean build docker.build docker.push

RM := rm -rf
WASM_PLUGIN_NAME := header-injector
DOCKER_IMAGE_HUB := localhost:5000
DOCKER_IMAGE_NAME := header-injector
DOCKER_IMAGE := $(DOCKER_IMAGE_HUB)/$(DOCKER_IMAGE_NAME)
VERSION_LIST := 0.0.1 0.0.2

all: docker.push

clean:
	$(RM) *.wasm
	
build: clean
	$(foreach VERSION, $(VERSION_LIST), tinygo build -ldflags '-X /main.injectionVersion=$(VERSION)' -o $(WASM_PLUGIN_NAME)-$(VERSION).wasm -scheduler=none -target=wasi plugin.go;)

docker.build: build
	$(foreach VERSION, $(VERSION_LIST), docker buildx build . -t $(DOCKER_IMAGE):$(VERSION) --build-arg WASM_BINARY=$(WASM_PLUGIN_NAME)-$(VERSION).wasm;)

docker.push: docker.build
	$(foreach VERSION, $(VERSION_LIST), docker push $(DOCKER_IMAGE):$(VERSION);)