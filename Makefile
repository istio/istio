########################
# Version info
########################

std_go_version := $(shell bin/get_protoc_gen_version.sh golang)
gogo_version := $(shell bin/get_protoc_gen_version.sh gogo)

########################
# protoc args
########################

proto_path := --proto_path=. --proto_path=vendor/github.com/gogo/protobuf --proto_path=vendor/istio.io/gogo-genproto/googleapis
out_path = :$(GOPATH)/src

########################
# protoc_gen_go
########################

protoc_gen_go_path := vendor/github.com/golang/protobuf
protoc_gen_go := bin/protoc-gen-go-$(std_go_version)
protoc_gen_go_prefix := --plugin=$(protoc_gen_go) --go-$(std_go_version)_out=plugins=grpc,
protoc_gen_go_plugin := $(protoc_gen_go_prefix)$(out_path)

########################
# protoc_gen_gogo*
########################

gogo_vendor_path := vendor/github.com/gogo/protobuf
protoc_gen_gogo := bin/protoc-gen-gogo-$(gogo_version)
protoc_gen_gogoslick := bin/protoc-gen-gogoslick-$(gogo_version)
protoc_min_version := bin/protoc-min-version-$(gogo_version)

gogo_plugin_prefix := --plugin=$(protoc_gen_gogo) --gogo-$(gogo_version)_out=plugins=grpc,
gogoslick_plugin_prefix := --plugin=$(protoc_gen_gogoslick) --gogoslick-$(gogo_version)_out=plugins=grpc,

comma := ,
empty:=
space := $(empty) $(empty)

importmaps := \
	gogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto \
	google/protobuf/any.proto=github.com/gogo/protobuf/types \
	google/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor \
	google/protobuf/duration.proto=github.com/gogo/protobuf/types \
	google/protobuf/timestamp.proto=github.com/gogo/protobuf/types \
	google/rpc/status.proto=istio.io/gogo-genproto/googleapis/google/rpc \
	google/rpc/code.proto=istio.io/gogo-genproto/googleapis/google/rpc \
	google/rpc/error_details.proto=istio.io/gogo-genproto/googleapis/google/rpc \

# generate mapping directive with M<proto>:<go pkg>, format for each proto file
mapping_with_spaces := $(foreach map,$(importmaps),M$(map),)
gogo_mapping := $(subst $(space),$(empty),$(mapping_with_spaces))

gogo_plugin := $(gogo_plugin_prefix)$(gogo_mapping)$(out_path)
gogoslick_plugin := $(gogoslick_plugin_prefix)$(gogo_mapping)$(out_path)


########################
# protoc
########################

protoc := $(protoc_min_version) -version=3.5.0

#####################
# install protoc
#####################

protoc_bin := $(shell which protoc)
# If protoc isn't on the path, set it to a target that's never up to date, so
# the install command always runs.
ifeq ($(protoc_bin),)
	protoc_bin = must-rebuild
endif

# Figure out which machine we're running on.
UNAME := $(shell uname)

# TODO add instructions for other operating systems here
$(protoc_bin):
ifeq ($(UNAME), Darwin)
	brew install protobuf
endif
ifeq ($(UNAME), Linux)
	curl -OL https://github.com/google/protobuf/releases/download/v3.5.0/protoc-3.5.0-linux-x86_64.zip
	unzip protoc-3.5.0-linux-x86_64.zip -d protoc3
	sudo mv protoc3/bin/* /usr/local/bin/
	sudo mv protoc3/include/* /usr/local/include/
	rm -f protoc-3.5.0-linux-x86_64.zip
	rm -rf protoc3
endif

###################
# Deps
###################

all: build

$(GOPATH)/bin/dep:
	go get -u github.com/golang/dep/cmd/dep

vendor: $(GOPATH)/bin/dep
	# Installing generation deps
	$(GOPATH)/bin/dep ensure -vendor-only

$(protoc_gen_go) : vendor
	@echo "Building protoc-gen-go..."
	@echo "Go version: " $(std_go_version)
	go build --pkgdir $(protoc_gen_go_path) -o $(protoc_gen_go) ./$(protoc_gen_go_path)/protoc-gen-go

$(protoc_gen_gogo) : vendor
	@echo "Building protoc-gen-gogo..."
	go build --pkgdir $(gogo_vendor_path)/protoc-gen-gogo -o $(protoc_gen_gogo) ./$(gogo_vendor_path)/protoc-gen-gogo

$(protoc_gen_gogoslick) : vendor
	@echo "Building protoc-gen-gogoslick..."
	go build --pkgdir $(gogo_vendor_path)/protoc-gen-gogoslick -o $(protoc_gen_gogoslick) ./$(gogo_vendor_path)/protoc-gen-gogoslick

$(protoc_min_version) : vendor
	@echo "Building protoc-min-version..."
	go build --pkgdir $(gogo_vendor_path)/protoc-min-version -o $(protoc_min_version) ./$(gogo_vendor_path)/protoc-min-version

binaries : $(protoc_gen_go) $(protoc_gen_gogo) $(protoc_gen_gogoslick) $(protoc_min_version)

depend: vendor binaries

#####################
# Generation Rules
#####################

generate: generate-broker-go generate-mesh-go generate-mixer-go generate-routing-go generate-rbac-go

#####################
# broker/...
#####################

broker_v1_protos := $(shell find broker/v1/config -type f -name '*.proto' | sort)
broker_v1_pb_gos := $(broker_v1_protos:.proto=.pb.go)

generate-broker-go: $(broker_v1_pb_gos)

$(broker_v1_pb_gos): $(broker_v1_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate broker/v1/config/*.pb.go
	$(protoc) $(proto_path) $(protoc_gen_go_plugin) $^

clean-broker-generated:
	rm $(broker_v1_pb_gos)

#####################
# mesh/...
#####################

mesh_protos := $(shell find mesh/v1alpha1 -type f -name '*.proto' | sort)
mesh_pb_gos := $(mesh_protos:.proto=.pb.go)

generate-mesh-go: $(mesh_pb_gos)

$(mesh_pb_gos): $(mesh_protos) | depend $(protoc_GEN_GO) $(protoc_bin)
	## Generate mesh/v1alpha1/*.pb.go
	$(protoc) $(proto_path) $(protoc_gen_go_plugin) $^

clean-mesh-generated:
	rm $(mesh_pb_gos)

#####################
# mixer/...
#####################

mixer_v1_protos :=  $(shell find mixer/v1 -maxdepth 1 -type f -name '*.proto' | sort)
mixer_v1_pb_gos := $(mixer_v1_protos:.proto=.pb.go)

mixer_config_client_protos := $(shell find mixer/v1/config/client -maxdepth 1 -type f -name '*.proto' | sort)
mixer_config_client_pb_gos := $(mixer_config_client_protos:.proto=.pb.go)

mixer_config_descriptor_protos := $(shell find mixer/v1/config/descriptor -maxdepth 1 -type f -name '*.proto' | sort)
mixer_config_descriptor_pb_gos := $(mixer_config_descriptor_protos:.proto=.pb.go)

mixer_template_protos := $(shell find mixer/v1/template -maxdepth 1 -type f -name '*.proto' | sort)
mixer_template_pb_gos := $(mixer_template_protos:.proto=.pb.go)

generate-mixer-go: $(mixer_v1_pb_gos) mixer/v1/config/fixed_cfg.pb.go $(mixer_config_client_pb_gos) $(mixer_config_descriptor_pb_gos) $(mixer_template_pb_gos)

$(mixer_v1_pb_gos): $(mixer_v1_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/v1/*.pb.go
	$(protoc) $(proto_path) $(gogoslick_plugin) $^

$(mixer_config_client_pb_gos) : $(mixer_config_client_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/v1/config/client/*.pb.go
	$(protoc) $(proto_path) $(gogoslick_plugin) $^

$(mixer_config_descriptor_pb_gos) : $(mixer_config_descriptor_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/v1/config/descriptor/*.pb.go
	$(protoc) $(proto_path) $(gogoslick_plugin) $^

$(mixer_template_pb_gos) : $(mixer_template_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/v1/template/*.pb.go
	$(protoc) $(proto_path) $(gogoslick_plugin) $^

mixer/v1/config/fixed_cfg.pb.go : mixer/v1/config/cfg.proto | depend $(protoc_gen_gogo) $(protoc_bin)
	# Generate mixer/v1/config/fixed_cfg.pb.go (requires alternate plugin and sed scripting due to issues with google.protobuf.Struct)
	$(protoc) $(proto_path) $(gogo_plugin) $^
	sed -e 's/*google_protobuf.Struct/interface{}/g' -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' mixer/v1/config/cfg.pb.go | goimports > mixer/v1/config/fixed_cfg.pb.go
	rm mixer/v1/config/cfg.pb.go

clean-mixer-generated:
	rm $(mixer_v1_pb_gos) $(mixer_config_client_pb_gos) $(mixer_config_descriptor_pb_gos) $(mixer_template_pb_gos) mixer/v1/config/fixed_cfg.pb.go

#####################
# routing/...
#####################

routing_v1alpha1_protos := $(shell find routing/v1alpha1 -type f -name '*.proto' | sort)
routing_v1alpha1_pb_gos := $(routing_v1alpha1_protos:.proto=.pb.go)

routing_v1alpha2_protos := $(shell find routing/v1alpha2 -type f -name '*.proto' | sort)
routing_v1alpha2_pb_gos := $(routing_v1alpha2_protos:.proto=.pb.go)

generate-routing-go: $(routing_v1alpha1_pb_gos) $(routing_v1alpha2_pb_gos)

$(routing_v1alpha1_pb_gos): $(routing_v1alpha1_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate routing/v1alpha1/*.pb.go
	$(protoc) $(proto_path) $(protoc_gen_go_plugin) $^

$(routing_v1alpha2_pb_gos): $(routing_v1alpha2_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate routing/v1alpha2/*.pb.go
	$(protoc) $(proto_path) $(protoc_gen_go_plugin) $^

clean-routing-generated:
	rm $(routing_v1alpha1_pb_gos) $(routing_v1alpha2_pb_gos)

#####################
# rbac/...
#####################

rbac_v1alpha1_protos := $(shell find rbac/v1alpha1 -type f -name '*.proto' | sort)
rbac_v1alpha1_pb_gos := $(rbac_v1alpha1_protos:.proto=.pb.go)

generate-rbac-go: $(protoc_gen_go) $(rbac_v1alpha1_pb_gos)

$(rbac_v1alpha1_pb_gos): $(rbac_v1alpha1_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate rbac/v1alpha1/*.pb.go
	$(protoc) $(proto_path) $(protoc_gen_go_plugin) $^

clean-rbac-generated:
	rm $(rbac_v1alpha1_pb_gos)

#####################
# Cleanup
#####################

clean:
	rm -rf bin/protoc-gen-*
	rm -rf bin/protoc-min-version-*
	rm -rf vendor

clean-generated: clean-broker-generated clean-mesh-generated clean-mixer-generated clean-routing-generated clean-rbac-generated