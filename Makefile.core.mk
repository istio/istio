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

HUB ?= gcr.io/istio-testing
TAG ?= 1.5-dev

pwd := $(shell pwd)

# make targets
.PHONY: lint lint-dependencies test_with_coverage mandiff build fmt vfsgen update-charts update-goldens

build: mesh

lint-dependencies:
	@! go mod graph | grep k8s.io/kubernetes || echo "depenency on k8s.io/kubernetes not allowed" || exit 2

lint: lint-copyright-banner lint-dependencies lint-go lint-python lint-scripts lint-yaml lint-dockerfiles lint-licenses

test:
	@go test -v -race ./...

test_with_coverage:
	@go test -race -coverprofile=coverage.txt -covermode=atomic ./...
	@curl -s https://codecov.io/bash | bash -s -- -c -F aFlag -f coverage.txt

mandiff: update-charts
	@scripts/run_mandiff.sh

fmt: format-go tidy-go

gen: generate-v1alpha1 generate-vfs tidy-go mirror-licenses

gen-check: clean gen check-clean-repo

clean: clean-values clean-vfs clean-charts

update-charts: installer.sha
	@scripts/run_update_charts.sh `cat installer.sha`

clean-charts:
	@rm -fr data/charts

generate-vfs: update-charts
	@go generate ./...

clean-vfs:
	@rm -fr pkg/vfs/assets.gen.go

mesh:
	# First line is for test environment, second is for target. Since these architectures can differ, the workaround
	# is to build both. TODO: figure out some way to implement this better, e.g. separate test target.
	go build -o $(GOBIN)/mesh ./cmd/mesh.go
	STATIC=0 GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) LDFLAGS='-extldflags -static -s -w' common/scripts/gobuild.sh $(TARGET_OUT)/mesh ./cmd/mesh.go

controller:
	go build -o $(GOBIN)/istio-operator ./cmd/manager
	STATIC=0 GOOS=$(TARGET_OS) GOARCH=$(TARGET_ARCH) LDFLAGS='-extldflags -static -s -w' common/scripts/gobuild.sh $(TARGET_OUT)/istio-operator ./cmd/manager

docker: controller
	mkdir -p $(GOBIN)/docker
	cp -a $(GOBIN)/istio-operator $(GOBIN)/docker/istio-operator
	cp -a build/Dockerfile $(GOBIN)/docker/Dockerfile.operator
	cp -aR build/bin $(GOBIN)/docker/bin
	cd $(GOBIN)/docker;docker build -t $(HUB)/operator:$(TAG) -f Dockerfile.operator .

docker.push:
	docker push $(HUB)/operator:$(TAG)

docker.save: docker
	mkdir -p $(TARGET_OUT)/release/docker
	docker save $(HUB)/operator:$(TAG) -o $(TARGET_OUT)/release/docker/operator.tar
	gzip --best $(TARGET_OUT)/release/docker/operator.tar

docker.all: docker docker.push

update-goldens:
	@UPDATE_GOLDENS=true go test -v ./cmd/mesh/...

e2e:
	@HUB=$(HUB) TAG=$(TAG) bash -c tests/e2e/e2e.sh

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

########################

v1alpha1_path := pkg/apis/istio/v1alpha1
v1alpha1_protos := $(wildcard $(v1alpha1_path)/*.proto)
v1alpha1_pb_gos := $(v1alpha1_protos:.proto=.pb.go)
v1alpha1_pb_pythons := $(patsubst $(v1alpha1_path)/%.proto,$(python_output_path)/$(v1alpha1_path)/%_pb2.py,$(v1alpha1_protos))
v1alpha1_pb_docs := $(v1alpha1_path)/v1alpha1.pb.html
v1alpha1_openapi := $(v1alpha1_protos:.proto=.json)

$(v1alpha1_pb_gos) $(v1alpha1_pb_docs) $(v1alpha1_pb_pythons): $(v1alpha1_protos)
	@$(protoc) $(go_plugin) $(protoc_gen_docs_plugin)$(v1alpha1_path) $(protoc_gen_python_plugin) $^
	@cp -r ${TMPDIR}/pkg/* pkg/
	@rm -fr ${TMPDIR}/pkg
	@go run $(repo_dir)/pkg/apis/istio/fixup_structs/main.go -f $(v1alpha1_path)/values_types.pb.go

generate-v1alpha1: $(v1alpha1_pb_gos) $(v1alpha1_pb_docs) $(v1alpha1_pb_pythons)

clean-values:
	@rm -fr $(v1alpha1_pb_gos) $(v1alpha1_pb_docs) $(v1alpha1_pb_pythons)

include common/Makefile.common.mk
