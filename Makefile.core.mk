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

TAG ?= latest

pwd := $(shell pwd)

# make targets
.PHONY: lint test_with_coverage mandiff build fmt vfsgen update-charts update-goldens

build: mesh

lint: lint-copyright-banner lint-go lint-python lint-scripts lint-yaml lint-dockerfiles lint-licenses

test:
	@go test -race ./...

test_with_coverage:
	@go test -race -coverprofile=coverage.txt -covermode=atomic ./...
	@curl -s https://codecov.io/bash | bash -s -- -c -F aFlag -f coverage.txt

mandiff: update-charts
	@PATH=${PATH}:${GOPATH}/bin scripts/run_mandiff.sh

fmt: format-go
	go mod tidy

update-charts: installer.sha
	@scripts/run_update_charts.sh `cat installer.sha`

clean-charts:
	@rm -fr data/charts
# make target dependencies
vfsgen: data/ update-charts
	go generate ./...

clean-vfs:
	@rm -fr pkg/vfs/assets.gen.go

gen: generate-values generate-types vfsgen
generate: gen

tidy:
	@go mod tidy

gen-check: clean gen tidy check-clean-repo

clean: clean-values clean-types clean-vfs clean-charts

default: mesh

mesh: vfsgen
	go build -o $(GOPATH)/bin/mesh ./cmd/mesh.go
	GOARCH=$(TARGET_ARCH) GOOS=$(TARGET_OS) go build -o /work/mesh ./cmd/mesh.go

# NOTE: docker targets only work with local builds.

controller: vfsgen
	go build -o $(GOPATH)/bin/istio-operator ./cmd/manager

docker: controller
	mkdir -p build/out
	cp $(GOPATH)/bin/istio-operator build/out/.
	docker build -t $(HUB)/operator:$(TAG) -f build/Dockerfile .

docker.push:
	docker push $(HUB)/operator:$(TAG)

docker.all: docker docker.push

update-goldens:
	export REFRESH_GOLDEN=true
	@go test ./cmd/mesh/...

########################

TMPDIR := $(shell mktemp -d)

repo_dir := .
out_path = ${TMPDIR}
protoc = protoc -Icommon-protos -I.

go_plugin_prefix := --go_out=plugins=grpc,
go_plugin := $(go_plugin_prefix):$(out_path)

python_output_path := python/istio_api
protoc_gen_python_prefix := --python_out=,
protoc_gen_python_plugin := $(protoc_gen_python_prefix):$(repo_dir)/$(python_output_path)

protoc_gen_docs_plugin := --docs_out=warnings=true,mode=html_fragment_with_front_matter:$(repo_dir)/

protoc_gen_k8s_support_plugins := --jsonshim_out=$(gogo_mapping):$(out_path) --deepcopy_out=$(gogo_mapping):$(out_path)

########################
types_v1alpha2_path := pkg/apis/istio/v1alpha2
types_v1alpha2_protos := $(wildcard $(types_v1alpha2_path)/*.proto)
types_v1alpha2_pb_gos := $(types_v1alpha2_protos:.proto=.pb.go)
types_v1alpha2_pb_pythons := $(patsubst $(types_v1alpha2_path)/%.proto,$(python_output_path)/$(types_v1alpha2_path)/%_pb2.py,$(types_v1alpha2_protos))
types_v1alpha2_pb_docs := $(types_v1alpha2_path)/v1alpha2.pb.html
types_v1alpha2_openapi := $(types_v1alpha2_protos:.proto=.json)

$(types_v1alpha2_pb_gos) $(types_v1alpha2_pb_docs) $(types_v1alpha2_pb_pythons): $(types_v1alpha2_protos)
	@$(protoc) $(go_plugin) $(protoc_gen_k8s_support_plugins) $(protoc_gen_docs_plugin)$(types_v1alpha2_path) $(protoc_gen_python_plugin) $^
	@cp -r ${TMPDIR}/pkg/* pkg/
	@sed -i 's|github.com/gogo/protobuf/protobuf/google/protobuf|github.com/gogo/protobuf/types|g' $(types_v1alpha2_path)/istiocontrolplane_types.pb.go
	@go run $(repo_dir)/pkg/apis/istio/fixup_structs/main.go -f $(types_v1alpha2_path)/istiocontrolplane_types.pb.go
	@sed -i 's|<key,value,effect>|\&lt\;key,value,effect\&gt\;|g' $(types_v1alpha2_path)/v1alpha2.pb.html
	@sed -i 's|<operator>|\&lt\;operator\&gt\;|g' $(types_v1alpha2_path)/v1alpha2.pb.html

generate-types: $(types_v1alpha2_pb_gos) $(types_v1alpha2_pb_docs) $(types_v1alpha2_pb_pythons)

clean-types:
	@rm -fr $(types_v1alpha2_pb_gos) $(types_v1alpha2_pb_docs) $(types_v1alpha2_pb_pythons)

values_v1alpha1_path := pkg/apis/istio/v1alpha1/
values_v1alpha1_protos := $(wildcard $(values_v1alpha1_path)/values_types*.proto)
values_v1alpha1_pb_gos := $(values_v1alpha1_protos:.proto=.pb.go)
values_v1alpha1_pb_pythons := $(patsubst $(values_v1alpha1_path)/%.proto,$(python_output_path)/$(values_v1alpha1_path)/%_pb2.py,$(values_v1alpha1_protos))
values_v1alpha1_pb_docs := $(values_v1alpha1_path)/v1alpha1.pb.html
values_v1alpha1_openapi := $(values_v1alpha1_protos:.proto=.json)

$(values_v1alpha1_pb_gos) $(values_v1alpha1_pb_docs) $(values_v1alpha1_pb_pythons): $(values_v1alpha1_protos)
	@$(protoc) $(go_plugin) $(protoc_gen_k8s_support_plugins) $(protoc_gen_docs_plugin)$(values_v1alpha1_path) $(protoc_gen_python_plugin) $^
	@cp -r ${TMPDIR}/pkg/* pkg/
	@sed -i 's|github.com/gogo/protobuf/protobuf/google/protobuf|github.com/gogo/protobuf/types|g' $(values_v1alpha1_path)/values_types.pb.go
	@GOARCH=amd64 GOOS=linux go run $(repo_dir)/pkg/apis/istio/fixup_structs/main.go -f $(values_v1alpha1_path)/values_types.pb.go

generate-values: $(values_v1alpha1_pb_gos) $(values_v1alpha1_pb_docs) $(values_v1alpha1_pb_pythons)

clean-values:
	@rm -fr $(values_v1alpha1_pb_gos) $(values_v1alpha1_pb_docs) $(values_v1alpha1_pb_pythons)

include common/Makefile.common.mk
