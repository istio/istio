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
build: inst build_api build_server
clean: clean_api clean_server
test: test_server

## API Targets

PROTOC = build/protoc.$(shell uname)
PROTO_OUTDIR_GO = api/v1/go
PROTO_OUTDIR_CPP = api/v1/cpp
PROTO_SRC = api/v1/service.proto api/v1/check.proto api/v1/report.proto api/v1/quota.proto

$(PROTO_OUTDIR_GO)/%.pb.go $(PROTO_OUTDIR_CPP)/%.pb.cc: api/v1/%.proto
	@echo "Building protos"
	@mkdir -p $(PROTO_OUTDIR_GO) $(PROTO_OUTDIR_CPP)
	@$(PROTOC) --proto_path=api/v1 --proto_path=vendor/github.com/googleapis/googleapis --proto_path=vendor/github.com/google/protobuf/src --cpp_out=$(PROTO_OUTDIR_CPP) --go_out=plugins=grpc:$(PROTO_OUTDIR_GO) $(PROTO_SRC)

build_api: $(PROTO_OUTDIR_GO)/service.pb.go

clean_api:
	@rm -fr $(PROTO_OUTDIR_GO) $(PROTO_OUTDIR_CPP)

## Server targets

GO_SRC = server/*.go adapters/*.go adapters/*/*.go

mixer.bin: $(GO_SRC) $(PROTO_SRC)
	@echo "Building server"
	@go build -o mixer.bin server/*.go

build_server: mixer.bin
	@go tool vet -shadowstrict server adapters
	@golint -set_exit_status server/...
	@golint -set_exit_status adapters/...
	@gofmt -w -s $(GO_SRC)

clean_server:
	@rm -f mixer.bin

test_server: build_server
	@echo "Running tests"
	@go test -race -cpu 1,4 ./server/... ./adapters/...

## Misc targets

GLIDE = build/glide.$(shell uname)

inst:
	@echo "Prepping dependencies"
	@$(GLIDE) -q install
	@ if ! which protoc-gen-go > /dev/null; then \
		echo "error: protoc-gen-go not installed" >&2;\
		go get github.com/golang/protobuf/protoc-gen-go;\
	fi
