# Copyright 2019 Istio Authors
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

export GO111MODULE=on
ifeq ($(BUILD_WITH_CONTAINER),0)
override GOBIN := $(GOPATH)/bin
endif

pwd := $(shell pwd)

# make targets
.PHONY: lint test_with_coverage mandiff build fmt vfsgen update-charts

lint: lint-copyright-banner lint-go

test:
	@go test -race ./...

test_with_coverage:
	@go test -race -coverprofile=coverage.txt -covermode=atomic ./...
	@curl -s https://codecov.io/bash | bash -s -- -c -F aFlag -f coverage.txt

mandiff: update-charts
	@PATH=${PATH}:${GOPATH}/bin scripts/run_mandiff.sh

build: mesh

fmt: format-go

update-charts: installer.sha
	@scripts/run_update_charts.sh `cat installer.sha`

# make target dependencies
vfsgen: data/ update-charts
	go generate ./...

generate: generate-values generate-types vfsgen

clean: clean-values clean-types

default: mesh

mesh: vfsgen
	go build -o ${GOBIN}/mesh ./cmd/mesh.go

########################

repo_dir := .
out_path = /tmp
protoc = protoc -I/usr/include/protobuf -I.

go_plugin_prefix := --go_out=plugins=grpc,
go_plugin := $(go_plugin_prefix):$(out_path)

python_output_path := python/istio_api
protoc_gen_python_prefix := --python_out=,
protoc_gen_python_plugin := $(protoc_gen_python_prefix):$(repo_dir)/$(python_output_path)

protoc_gen_docs_plugin := --docs_out=warnings=true,mode=html_fragment_with_front_matter:$(repo_dir)/

types_v1alpha2_path := pkg/apis/istio/v1alpha2
types_v1alpha2_protos := $(wildcard $(types_v1alpha2_path)/*.proto)
types_v1alpha2_pb_gos := $(types_v1alpha2_protos:.proto=.pb.go)
types_v1alpha2_pb_pythons := $(patsubst $(types_v1alpha2_path)/%.proto,$(python_output_path)/$(types_v1alpha2_path)/%_pb2.py,$(types_v1alpha2_protos))
types_v1alpha2_pb_docs := $(types_v1alpha2_protos:.proto=.pb.html)
types_v1alpha2_openapi := $(types_v1alpha2_protos:.proto=.json)

$(types_v1alpha2_pb_gos) $(types_v1alpha2_pb_docs) $(types_v1alpha2_pb_pythons): $(types_v1alpha2_protos)
	@$(protoc) $(go_plugin) $(protoc_gen_docs_plugin)$(types_v1alpha2_path) $(protoc_gen_python_plugin) $^
	@cp -r /tmp/pkg/* pkg/
	@sed -i -e 's|github.com/gogo/protobuf/protobuf/google/protobuf|github.com/gogo/protobuf/types|g' $(types_v1alpha2_path)/istiocontrolplane_types.pb.go
	@patch $(types_v1alpha2_path)/istiocontrolplane_types.pb.go < $(types_v1alpha2_path)/fixup_go_structs.patch

generate-types: $(types_v1alpha2_pb_gos) $(types_v1alpha2_pb_docs) $(types_v1alpha2_pb_pythons)

clean-types:
	@rm -fr $(types_v1alpha2_pb_gos) $(types_v1alpha2_pb_docs) $(types_v1alpha2_pb_pythons)

values_v1alpha2_path := pkg/apis/istio/v1alpha2/values
values_v1alpha2_protos := $(wildcard $(values_v1alpha2_path)/*.proto)
values_v1alpha2_pb_gos := $(values_v1alpha2_protos:.proto=.pb.go)
values_v1alpha2_pb_pythons := $(patsubst $(values_v1alpha2_path)/%.proto,$(python_output_path)/$(values_v1alpha2_path)/%_pb2.py,$(values_v1alpha2_protos))
values_v1alpha2_pb_docs := $(values_v1alpha2_protos:.proto=.pb.html)
values_v1alpha2_openapi := $(values_v1alpha2_protos:.proto=.json)

$(values_v1alpha2_pb_gos) $(values_v1alpha2_pb_docs) $(values_v1alpha2_pb_pythons): $(values_v1alpha2_protos)
	@$(protoc) $(go_plugin) $(protoc_gen_docs_plugin)$(values_v1alpha2_path) $(protoc_gen_python_plugin) $^
	@cp -r /tmp/pkg/* pkg/
	@sed -i -e 's|github.com/gogo/protobuf/protobuf/google/protobuf|github.com/gogo/protobuf/types|g' $(values_v1alpha2_path)/values_types.pb.go
	@patch $(values_v1alpha2_path)/values_types.pb.go < $(values_v1alpha2_path)/fix_values_structs.patch

generate-values: $(values_v1alpha2_pb_gos) $(values_v1alpha2_pb_docs) $(values_v1alpha2_pb_pythons)

clean-values:
	@rm -fr $(values_v1alpha2_pb_gos) $(values_v1alpha2_pb_docs) $(values_v1alpha2_pb_pythons)

include common/Makefile.common.mk
