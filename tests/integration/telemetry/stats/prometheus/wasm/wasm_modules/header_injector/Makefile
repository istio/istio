.PHONY: docker-push docker-build build clean

VERSION_LIST := 1 2
HUB ?= gcr.io/istio-testing
IMAGE_PREFIX ?= /wasm
IMG := $(HUB)$(IMAGE_PREFIX)/header-injector

all: docker-push

build: plugin.cc plugin.h BUILD WORKSPACE
	rm -f *.wasm
	$(foreach VERSION, $(VERSION_LIST), bazel build :plugin-0.0.$(VERSION).wasm && cp bazel-bin/plugin-0.0.$(VERSION).wasm .;)

docker-build: build
	$(foreach VERSION, $(VERSION_LIST), docker buildx build . -t $(IMG):0.0.$(VERSION) --build-arg WASM_BINARY=plugin-0.0.$(VERSION).wasm;)

docker-push: docker-build
	$(foreach VERSION, $(VERSION_LIST), docker push $(IMG):0.0.$(VERSION);)

clean:
	rm -rf bazel-*
	rm -f *.wasm
