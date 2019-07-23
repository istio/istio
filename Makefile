export GO111MODULE=on

gen_img := gcr.io/istio-testing/protoc:2019-03-29
pwd := $(shell pwd)
mount_dir := /src
uid := $(shell id -u)
docker_gen := docker run --rm --user $(uid) -v /etc/passwd:/etc/passwd:ro -v $(pwd):$(mount_dir) -w $(mount_dir) $(gen_img) -I.
out_path = .

########################
# protoc_gen_gogo*
########################

#gofast_plugin_prefix := --gofast_out=plugins=grpc,
gogofast_plugin_prefix := --gogofast_out=plugins=grpc,

comma := ,
empty:=
space := $(empty) $(empty)

importmaps := \
	gogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto \
	google/protobuf/any.proto=github.com/gogo/protobuf/types \
	google/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor \
	google/protobuf/duration.proto=github.com/gogo/protobuf/types \
	google/protobuf/struct.proto=github.com/gogo/protobuf/types \
	google/protobuf/timestamp.proto=github.com/gogo/protobuf/types \
	google/protobuf/wrappers.proto=github.com/gogo/protobuf/types \
	google/rpc/status.proto=github.com/gogo/googleapis/google/rpc \
	google/rpc/code.proto=github.com/gogo/googleapis/google/rpc \
	google/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc \

# generate mapping directive with M<proto>:<go pkg>, format for each proto file
mapping_with_spaces := $(foreach map,$(importmaps),M$(map),)
gogo_mapping := $(subst $(space),$(empty),$(mapping_with_spaces))

#gofast_plugin := $(gofast_plugin_prefix)$(gogo_mapping):$(out_path)
gogofast_plugin := $(gogofast_plugin_prefix)$(gogo_mapping):$(out_path)

#####################
# Generation Rules
#####################

api_path := pkg/apis/istio/v1alpha2
api_protos := $(shell find $(api_path) -type f -name '*.proto' | sort)
api_pb_gos := $(api_protos:.proto=.pb.go)

.PHONY: default lint mandiff coverage test build

default: iop

generate-api-go: $(api_pb_gos)
	patch pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go < pkg/apis/istio/v1alpha2/fixup_go_structs.patch

$(api_pb_gos): $(api_protos)
	@$(docker_gen) $(gogofast_plugin) $^

clean-proto:
	rm -f $(api_pb_gos)

fmt:
	@scripts/run_gofmt.sh


include Makefile.common.mk

# CI Targets
lint:
	# These PATH hacks are temporary until prow properly sets its paths
	@PATH=${PATH}:${GOPATH}/bin scripts/check_license.sh
	@PATH=${PATH}:${GOPATH}/bin scripts/run_golangci.sh

mandiff:
	# These PATH hacks are temporary until prow properly sets its paths
	@PATH=${PATH}:${GOPATH}/bin scripts/run_mandiff.sh

coverage:
	@scripts/codecov.sh

test:
	GO111MODULE=on go test -race ./...

build: iop

# get imported protos to $GOPATH
get_dep_proto:
	GO111MODULE=off go get k8s.io/api/core/v1 k8s.io/api/autoscaling/v2beta1 k8s.io/apimachinery/pkg/apis/meta/v1/ github.com/gogo/protobuf/...

proto_iscp:
	protoc -I=${GOPATH}/src -I./pkg/apis/istio/v1alpha2/ --proto_path=pkg/apis/istio/v1alpha2/ --gofast_out=pkg/apis/istio/v1alpha2/ pkg/apis/istio/v1alpha2/istiocontrolplane_types.proto
	sed -i -e 's|github.com/gogo/protobuf/protobuf/google/protobuf|github.com/gogo/protobuf/types|g' pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go
	patch pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go < pkg/apis/istio/v1alpha2/fixup_go_structs.patch

proto_iscp_orig:
	protoc -I=${GOPATH}/src -I./pkg/apis/istio/v1alpha2/ --proto_path=pkg/apis/istio/v1alpha2/ --gofast_out=pkg/apis/istio/v1alpha2/ pkg/apis/istio/v1alpha2/istiocontrolplane_types.proto
	sed -i -e 's|github.com/gogo/protobuf/protobuf/google/protobuf|github.com/gogo/protobuf/types|g' pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go
	cp pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go.orig

proto_values:
	protoc -I=${GOPATH}/src -I./pkg/apis/istio/v1alpha2/values --proto_path=pkg/apis/istio/v1alpha2/values --go_out=pkg/apis/istio/v1alpha2/values pkg/apis/istio/v1alpha2/values/values_types.proto
	sed -i -e 's|github.com/gogo/protobuf/protobuf/google/protobuf|github.com/gogo/protobuf/types|g' pkg/apis/istio/v1alpha2/values/values_types.pb.go
	patch pkg/apis/istio/v1alpha2/values/values_types.pb.go < pkg/apis/istio/v1alpha2/values/fix_values_structs.patch

proto_values_orig:
	protoc -I=${GOPATH}/src -I./pkg/apis/istio/v1alpha2/values --proto_path=pkg/apis/istio/v1alpha2/values --go_out=pkg/apis/istio/v1alpha2/values pkg/apis/istio/v1alpha2/values/values_types.proto
	sed -i -e 's|github.com/gogo/protobuf/protobuf/google/protobuf|github.com/gogo/protobuf/types|g' pkg/apis/istio/v1alpha2/values/values_types.pb.go
	cp pkg/apis/istio/v1alpha2/values/values_types.pb.go pkg/apis/istio/v1alpha2/values/values_types.pb.go.orig

proto_iscporig_setup: get_dep_proto proto_iscp_orig

proto_iscp_setup: get_dep_proto proto_iscp

proto_values_setup: get_dep_proto proto_values

proto_valuesorig_setup: get_dep_proto proto_values_orig

gen_patch_iscp:
	diff -u pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go.orig pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go > pkg/apis/istio/v1alpha2/fixup_go_structs.patch || true

gen_patch_values:
	diff -u pkg/apis/istio/v1alpha2/values/values_types.pb.go.orig pkg/apis/istio/v1alpha2/values/values_types.pb.go > pkg/apis/istio/v1alpha2/values/fix_values_structs.patch || true

vfsgen: data/
	go get github.com/shurcooL/vfsgen
	go generate ./cmd/iop.go

iop: vfsgen
	go build -o ${GOPATH}/bin/iop ./cmd/iop.go
