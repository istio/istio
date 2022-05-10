.PHONY: docker-build build

VERSION_LIST := 1 2
HUB ?= gcr.io/istio-testing
IMAGE_PREFIX ?= /wasm
IMG := $(HUB)$(IMAGE_PREFIX)/header-injector

all: docker-push

target/wasm32-unknown-unknown/debug/plugin.wasm:
	cargo build --target wasm32-unknown-unknown
target/wasm32-unknown-unknown/release/plugin.wasm:
	cargo build --release --target wasm32-unknown-unknown

local-build-debug: target/wasm32-unknown-unknown/debug/plugin.wasm
local-build-release: target/wasm32-unknown-unknown/release/plugin.wasm

docker-build:
	$(foreach VERSION, $(VERSION_LIST), docker buildx build . -t $(IMG):0.0.$(VERSION) --build-arg INJECTION_HEADER_VALUE=0.0.$(VERSION);)

docker-push: docker-build
	$(foreach VERSION, $(VERSION_LIST), docker push $(IMG):0.0.$(VERSION);)

clean:
	rm -rf target
