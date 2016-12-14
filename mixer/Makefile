## Copyright 2016 Google Inc.
##
## Licensed under the Apache License, Version 2.0 (the "License");
## you may not use this file except in compliance with the License.
## You may obtain a copy of the License at
##
##     http://www.apache.org/licenses/LICENSE-2.0
##
## Unless required by applicable law or agreed to in writing, software
## distributed under the License is distributed on an "AS IS" BASIS,
## WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
## See the License for the specific language governing permissions and
## limitations under the License.

# Primary build targets

TOP := $(shell pwd)
MIXREPO := istio.io/mixer

build: check_env dep build_api build_config build_server build_client
clean: check_env clean_api clean_config clean_server clean_client
	@go clean $(CLEAN_FLAGS)
	@go clean $(CLEAN_FLAGS) -i ./...
test: build test_server

check_env:
ifdef VERBOSE
BUILD_FLAGS := "-v"
CLEAN_FLAGS := "-x"
endif
ifeq (,$(findstring $(MIXREPO),$(TOP)))
	$(error project should be built at $(GOPATH)/src/$(MIXREPO))
endif


## API Targets

PROTOC = bin/protoc.$(shell uname)
VENDOR = vendor

API_SRC = $(wildcard api/v1/*.proto)
API_OUTDIR_BASE = $(CURDIR)
API_OUTDIR_FULL = $(API_OUTDIR_BASE)/api/v1
API_OUTPUTS = $(API_SRC:api/v1/%.proto=$(API_OUTDIR_FULL)/%.pb.go)

$(API_OUTDIR_FULL)/%.pb.go: api/v1/%.proto
	@echo "Building API protos"
	@mkdir -p $(API_OUTDIR_FULL)
	@$(PROTOC) --proto_path=. --proto_path=vendor/github.com/googleapis/googleapis --proto_path=vendor/github.com/google/protobuf/src --go_out=plugins=grpc:$(API_OUTDIR_BASE) $(API_SRC)

build_api: $(API_OUTPUTS)

clean_api:
	@rm -fr $(API_OUTDIR_FULL)/*.pb.go

## Config Targets

CONFIG_SRC = $(wildcard config/v1/*.proto)
CONFIG_OUTDIR_BASE = $(CURDIR)
CONFIG_OUTDIR_FULL = $(CONFIG_OUTDIR_BASE)/config/v1
CONFIG_OUTPUTS = $(CONFIG_SRC:config/v1/%.proto=$(CONFIG_OUTDIR_FULL)/%.pb.go)

$(CONFIG_OUTDIR_FULL)/%.pb.go: config/v1/%.proto
	@echo "Building config protos"
	@mkdir -p $(CONFIG_OUTDIR_FULL)
	@$(PROTOC) --proto_path=. --proto_path=vendor/github.com/googleapis/googleapis --proto_path=vendor/github.com/google/protobuf/src --go_out=plugins=grpc:$(CONFIG_OUTDIR_BASE) $(CONFIG_SRC)

build_config: $(CONFIG_OUTPUTS)

clean_config:
	@rm -fr $(CONFIG_OUTDIR_FULL)/*.pb.go

## Server targets

SERVER_SRC = server/*.go adapters/*.go adapters/*/*.go

mixer.bin: $(SERVER_SRC) $(API_OUTPUTS) $(CONFIG_OUTPUTS)
	@echo "Building server"
	@go build -i $(BUILD_FLAGS) -o mixer.bin server/*.go

build_server: mixer.bin
	@go tool vet -shadowstrict server adapters
	@golint -set_exit_status server/...
	@golint -set_exit_status adapters/...
	@gofmt -w -s $(SERVER_SRC)

clean_server:
	@go clean $(CLEAN_FLAGS) -i ./server/... ./adapters/...
	@rm -f mixer.bin

test_server:
	@echo "Running tests"
	@go test -race -cpu 1,4 ./server/... ./adapters/...

## Client targets

CLIENT_SRC = example/client/*.go

client.bin: $(CLIENT_SRC) $(API_OUTPUTS)
	@echo "Building client"
	@go build -i $(BUILD_FLAGS) -o client.bin example/client/*.go

build_client: client.bin
	@go tool vet -shadowstrict example/client
	@golint -set_exit_status example/client/...
	@gofmt -w -s $(CLIENT_SRC)

clean_client:
	@go clean $(CLEAN_FLAGS) -i ./example/client/...
	@rm -f client.bin

## Misc targets

GLIDE = third_party/bin/glide.$(shell uname)

dep:
	@echo "Prepping dependencies"
	@$(GLIDE) -q install
	@ if ! which protoc-gen-go > /dev/null; then \
		echo "warning: protoc-gen-go not installed, attempting install" >&2;\
		go get github.com/golang/protobuf/protoc-gen-go;\
	fi
