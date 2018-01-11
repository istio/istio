# GOOGLEAPIS 
GOOGLEAPIS_SHA := c8c975543a134177cc41b64cbbf10b88fe66aa1d
GOOGLEAPIS_URL := https://raw.githubusercontent.com/googleapis/googleapis/$(GOOGLEAPIS_SHA)

# BUILD UP PROTOC ARGs
PROTO_PATH := --proto_path=protoc-tmp --proto_path=. --proto_path=vendor/github.com/gogo/protobuf

RPC_PATH := google/rpc
STATUS_PROTO := protoc-tmp/$(RPC_PATH)/status.proto
CODE_PROTO := protoc-tmp/$(RPC_PATH)/code.proto
ERR_PROTO := protoc-tmp/$(RPC_PATH)/error_details.proto

STD_GO_VERSION := $(shell bin/get_protoc_gen_version.sh golang)
GOGO_VERSION := $(shell bin/get_protoc_gen_version.sh gogo)

STD_GO_PATH := vendor/github.com/golang/protobuf
STD_GO := protoc-gen-go
PROTOC_GEN_GO := bin/$(STD_GO)-$(STD_GO_VERSION)

GOGOPROTO_PATH := vendor/github.com/gogo/protobuf
GOGO := protoc-gen-gogo
GOGO_PATH := $(GOGOPROTO_PATH)/$(GOGO)
GOGOSLICK := protoc-gen-gogoslick
GOGOSLICK_PATH := $(GOGOPROTO_PATH)/$(GOGOSLICK)
PROTOC_MIN_VERSION := protoc-min-version
MIN_VERSION_PATH := $(GOGOPROTO_PATH)/$(PROTOC_MIN_VERSION)
PROTOC_GEN_GOGO := bin/$(GOGO)-$(GOGO_VERSION)
PROTOC_GEN_GOGOSLICK := bin/$(GOGOSLICK)-$(GOGO_VERSION)
PROTOC_MIN_VERSION_VERSION := bin/$(PROTOC_MIN_VERSION)-$(GOGO_VERSION)

PROTOC := $(PROTOC_MIN_VERSION_VERSION) -version=3.5.0

STD_GO_PLUGIN_PREFIX := --plugin=bin/$(STD_GO)-$(STD_GO_VERSION) --go-$(STD_GO_VERSION)_out=plugins=grpc,
GOGOSLICK_PLUGIN_PREFIX := --plugin=bin/$(GOGOSLICK)-$(GOGO_VERSION) --gogoslick-$(GOGO_VERSION)_out=plugins=grpc,
GOGO_PLUGIN_PREFIX := --plugin=bin/$(GOGO)-$(GOGO_VERSION) --gogo-$(GOGO_VERSION)_out=plugins=grpc,
PLUGIN_SUFFIX = :.

# BASIC STANDARD MAPPINGS
comma := ,
empty:=
space := $(empty) $(empty)

GOGO_PROTO_PKG := github.com/gogo/protobuf/gogoproto
GOGO_TYPES := github.com/gogo/protobuf/types
GOGO_DESCRIPTOR := github.com/gogo/protobuf/protoc-gen-gogo/descriptor
GOGO_GOOGLEAPIS_RPC := istio.io/gogo-genproto/googleapis/google/rpc

importmaps := \
	gogoproto/gogo.proto=$(GOGO_PROTO_PKG) \
	google/protobuf/any.proto=$(GOGO_TYPES) \
	google/protobuf/descriptor.proto=$(GOGO_DESCRIPTOR) \
	google/protobuf/duration.proto=$(GOGO_TYPES) \
	google/protobuf/timestamp.proto=$(GOGO_TYPES) \
	google/rpc/status.proto=$(GOGO_GOOGLEAPIS_RPC) \
	google/rpc/code.proto=$(GOGO_GOOGLEAPIS_RPC) \
	google/rpc/error_details.proto=$(GOGO_GOOGLEAPIS_RPC) \

# generate mapping directive with M<proto>:<go pkg>, format for each proto file
mapping_with_spaces := $(foreach map,$(importmaps),M$(map),)
MAPPING := $(subst $(space),$(empty),$(mapping_with_spaces))
STD_GO_PLUGIN := $(STD_GO_PLUGIN_PREFIX)$(PLUGIN_SUFFIX)
GOGOSLICK_PLUGIN := $(GOGOSLICK_PLUGIN_PREFIX)$(MAPPING)$(PLUGIN_SUFFIX)
GOGO_PLUGIN := $(GOGO_PLUGIN_PREFIX)$(MAPPING)$(PLUGIN_SUFFIX)

#####################
# Broker
#####################

BROKER_V1_CONFIG := broker/v1/config
SERVICE_CLASS_PROTO := $(BROKER_V1_CONFIG)/service_class.proto
SERVICE_PLAN_PROTO := $(BROKER_V1_CONFIG)/service_plan.proto
BROKER_V1_PROTOS := $(SERVICE_CLASS_PROTO) $(SERVICE_PLAN_PROTO)
BROKER_V1_PB_GOS := $(BROKER_V1_PROTOS:.proto=.pb.go)

#####################
# Mesh
#####################

MESH_V1ALPHA1 := mesh/v1alpha1
MESH_CONFIG_PROTO := $(MESH_V1ALPHA1)/config.proto
MESH_PROTOS := $(MESH_CONFIG_PROTO)
MESH_PB_GOS := $(MESH_PROTOS:.proto=.pb.go)

#####################
# Mixer
#####################

MIXER_V1 := mixer/v1
ATTR_PROTO := $(MIXER_V1)/attributes.proto
CHECK_PROTO := $(MIXER_V1)/check.proto
REPORT_PROTO := $(MIXER_V1)/report.proto
SVC_PROTO := $(MIXER_V1)/service.proto
MIXER_V1_PROTOS := $(ATTR_PROTO) $(CHECK_PROTO) $(REPORT_PROTO) $(SVC_PROTO)
MIXER_V1_PB_GOS := $(MIXER_V1_PROTOS:.proto=.pb.go)

MIXER_CONFIG := $(MIXER_V1)/config
CFG_PROTO := $(MIXER_CONFIG)/cfg.proto
MIXER_CONFIG_PROTOS := $(CFG_PROTO)
MIXER_CONFIG_PB_GOS := $(MIXER_CONFIG_PROTOS:.proto=.pb.go)

MIXER_CONFIG_CLIENT := $(MIXER_CONFIG)/client
API_SPEC_PROTO := $(MIXER_CONFIG_CLIENT)/api_spec.proto
AUTH_PROTO := $(MIXER_CONFIG_CLIENT)/auth.proto
CLIENT_CONFIG_PROTO := $(MIXER_CONFIG_CLIENT)/client_config.proto
QUOTA_PROTO := $(MIXER_CONFIG_CLIENT)/quota.proto
CONFIG_SVC_PROTO := $(MIXER_CONFIG_CLIENT)/service.proto
MIXER_CONFIG_CLIENT_PROTOS := $(API_SPEC_PROTO) $(AUTH_PROTO) $(CLIENT_CONFIG_PROTO) $(QUOTA_PROTO) $(CONFIG_SVC_PROTO)
MIXER_CONFIG_CLIENT_PB_GOS := $(MIXER_CONFIG_CLIENT_PROTOS:.proto=.pb.go)

MIXER_CONFIG_DESCRIPTOR := $(MIXER_CONFIG)/descriptor
VALUE_TYPE_PROTO := $(MIXER_CONFIG_DESCRIPTOR)/value_type.proto
MIXER_CONFIG_DESCRIPTOR_PROTOS := $(VALUE_TYPE_PROTO)
MIXER_CONFIG_DESCRIPTOR_PB_GOS := $(MIXER_CONFIG_DESCRIPTOR_PROTOS:.proto=.pb.go)

MIXER_TEMPLATE := mixer/v1/template
TEMPLATE_EXT_PROTO := $(MIXER_TEMPLATE)/extensions.proto
STD_TYPES_PROTO := $(MIXER_TEMPLATE)/standard_types.proto
MIXER_TEMPLATE_PROTOS := $(TEMPLATE_EXT_PROTO) $(STD_TYPES_PROTO)
MIXER_TEMPLATE_PB_GOS := $(MIXER_TEMPLATE_PROTOS:.proto=.pb.go)

## MIXER-PROTO SPECIFIC MAPPINGS
MIXER_V1_PKG := istio.io/api/mixer/v1
CONFIG_PKG := istio.io/api/mixer/v1/config
CLIENT_PKG := istio.io/api/mixer/v1/config/client
DESCRIPTOR_PKG := istio.io/api/mixer/v1/config/descriptor
TEMPLATE_PKG := istio.io/api/mixer/v1/template

mixer_importmaps = \
	$(ATTR_PROTO)=$(MIXER_V1_PKG) \
	$(CHECK_PROTO)=$(MIXER_V1_PKG) \
	$(REPORT_PROTO)=$(MIXER_V1_PKG) \
	$(SVC_PROTO)=$(MIXER_V1_PKG) \
	$(API_SPEC_PROTO)=$(CLIENT_PKG) \
	$(AUTH_PROTO)=$(CLIENT_PKG) \
	$(CFG_PROTO)=$(CONFIG_PKG) \
	$(CLIENT_CONFIG_PROTO)=$(CLIENT_PKG) \
	$(QUOTA_PROTO)=$(CLIENT_PKG) \
	$(CONFIG_SVC_PROTO)=$(CLIENT_PKG) \
	$(VALUE_TYPE_PROTO)=$(DESCRIPTOR_PKG) \
	$(TEMPLATE_EXT_PROTO)=$(TEMPLATE_PKG) \
	$(STD_TYPES_PROTO)=$(TEMPLATE_PKG)

# generate mapping directive with M<proto>:<go pkg>, format for each proto file
mixer_mapping_with_spaces := $(foreach map,$(mixer_importmaps),M$(map),)
MIXER_MAPPING := $(MAPPING)$(subst $(space),$(empty),$(mixer_mapping_with_spaces))

MIXER_PLUGIN := $(GOGOSLICK_PLUGIN_PREFIX)$(MIXER_MAPPING)$(PLUGIN_SUFFIX)
ALT_MIXER_PLUGIN := $(GOGO_PLUGIN_PREFIX)$(MIXER_MAPPING)$(PLUGIN_SUFFIX)

#####################
# Routing
#####################

ROUTING_V1ALPHA1 := routing/v1alpha1
ROUTING_V1ALPHA2 := routing/v1alpha2

ROUTING_V1ALPHA1_DEST_POLICY_PROTO := $(ROUTING_V1ALPHA1)/dest_policy.proto
ROUTING_V1ALPHA1_EGRESS_PROTO := $(ROUTING_V1ALPHA1)/egress_rule.proto
ROUTING_V1ALPHA1_HTTP_FAULT_PROTO := $(ROUTING_V1ALPHA1)/http_fault.proto
ROUTING_V1ALPHA1_INGRESS_PROTO := $(ROUTING_V1ALPHA1)/ingress_rule.proto
ROUTING_V1ALPHA1_L4_FAULT_PROTO := $(ROUTING_V1ALPHA1)/l4_fault.proto
ROUTING_V1ALPHA1_ROUTE_PROTO := $(ROUTING_V1ALPHA1)/route_rule.proto
ROUTING_V1ALPHA1_PROTOS := $(ROUTING_V1ALPHA1_DEST_POLICY_PROTO) $(ROUTING_V1ALPHA1_EGRESS_PROTO) $(ROUTING_V1ALPHA1_HTTP_FAULT_PROTO) $(ROUTING_V1ALPHA1_INGRESS_PROTO) $(ROUTING_V1ALPHA1_L4_FAULT_PROTO) $(ROUTING_V1ALPHA1_ROUTE_PROTO)
ROUTING_V1ALPHA1_PB_GOS := $(ROUTING_V1ALPHA1_PROTOS:.proto=.pb.go)

ROUTING_V1ALPHA2_DESTINATION_RULE_PROTO := $(ROUTING_V1ALPHA2)/destination_rule.proto
ROUTING_V1ALPHA2_GATEWAY_PROTO := $(ROUTING_V1ALPHA2)/gateway.proto
ROUTING_V1ALPHA2_ROUTE_PROTO := $(ROUTING_V1ALPHA2)/route_rule.proto
ROUTING_V1ALPHA2_FOREIGN_SERVICES_PROTO := $(ROUTING_V1ALPHA2)/foreign_services.proto
ROUTING_V1ALPHA2_PROTOS := $(ROUTING_V1ALPHA2_DESTINATION_RULE_PROTO) $(ROUTING_V1ALPHA2_GATEWAY_PROTO) $(ROUTING_V1ALPHA2_ROUTE_PROTO) $(ROUTING_V1ALPHA2_FOREIGN_SERVICES_PROTO)
ROUTING_V1ALPHA2_PB_GOS := $(ROUTING_V1ALPHA2_PROTOS:.proto=.pb.go)

#####################
# RBAC
#####################

RBAC_V1 := rbac/v1
RBAC_PROTO := $(RBAC_V1)/rbac.proto
RBAC_V1_PROTOS := $(RBAC_PROTO)
RBAC_V1_PB_GOS := $(RBAC_V1_PROTOS:.proto=.pb.go)

#####################
# Install protoc
#####################

PROTOC_BIN := $(shell which protoc)
# If protoc isn't on the path, set it to a target that's never up to date, so
# the install command always runs.
ifeq ($(PROTOC_BIN),)
	PROTOC_BIN = must-rebuild
endif

# Figure out which machine we're running on.
UNAME := $(shell uname)

# TODO add instructions for other operating systems here
$(PROTOC_BIN):
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


#####################
# Generation Rule
#####################

generate: generate-broker-go generate-mesh-go generate-mixer-go generate-routing-go generate-rbac-go

$(GOPATH)/bin/dep:
	go get -u github.com/golang/dep/cmd/dep

$(PROTOC_GEN_GO) : vendor
	@echo "Building protoc-gen-go..."
	@echo "Go version: " $(STD_GO_VERSION)
	go build --pkgdir $(STD_GO_PATH) -o $(PROTOC_GEN_GO) ./$(STD_GO_PATH)/$(STD_GO)

$(PROTOC_GEN_GOGO) : vendor
	@echo "Building protoc-gen-gogo..."
	go build --pkgdir $(GOGO_PATH) -o $(PROTOC_GEN_GOGO) ./$(GOGOPROTO_PATH)/$(GOGO)

$(PROTOC_GEN_GOGOSLICK) : vendor
	@echo "Building protoc-gen-gogoslick..."
	go build --pkgdir $(GOGOSLICK_PATH) -o $(PROTOC_GEN_GOGOSLICK) ./$(GOGOPROTO_PATH)/$(GOGOSLICK)

$(PROTOC_MIN_VERSION_VERSION) : vendor
	@echo "Building protoc-min-version..."
	go build --pkgdir $(MIN_VERSION_PATH) -o $(PROTOC_MIN_VERSION_VERSION) ./$(GOGOPROTO_PATH)/$(PROTOC_MIN_VERSION)

binaries : $(PROTOC_GEN_GO) $(PROTOC_GEN_GOGO) $(PROTOC_GEN_GOGOSLICK) $(PROTOC_MIN_VERSION_VERSION)

vendor: $(GOPATH)/bin/dep
	# Installing generation deps
	$(GOPATH)/bin/dep ensure -vendor-only

install-deps: vendor binaries

protoc.version:
	@echo "Using protoc version:" `protoc --version`

protoc-tmp:
	mkdir -p protoc-tmp

protoc-tmp/$(RPC_PATH): protoc-tmp
	mkdir -p protoc-tmp/$(RPC_PATH)

download-googleapis-protos: $(STATUS_PROTO) $(CODE_PROTO) $(ERR_PROTO)

$(STATUS_PROTO): protoc-tmp/$(RPC_PATH)
	# Downloading google/rpc/status.proto
	curl -sS $(GOOGLEAPIS_URL)/google/rpc/status.proto -o $(STATUS_PROTO)

$(CODE_PROTO): protoc-tmp/$(RPC_PATH)
	# Downloading google/rpc/code.proto
	curl -sS $(GOOGLEAPIS_URL)/google/rpc/code.proto -o $(CODE_PROTO)

$(ERR_PROTO): protoc-tmp/$(RPC_PATH)
	# Downloading google/rpc/error_details.proto
	curl -sS $(GOOGLEAPIS_URL)/google/rpc/error_details.proto -o $(ERR_PROTO)

#####################
# mixer/...
#####################

generate-mixer-go: install-deps download-googleapis-protos protoc.version $(PROTOC_GEN_GOGOSLICK) $(PROTOC_GEN_GOGO) generate-mixer-v1-go generate-mixer-v1-config-go generate-mixer-v1-config-client-go generate-mixer-v1-template-go

generate-mixer-v1-go: $(MIXER_V1_PB_GOS)

generate-mixer-v1-config-go: mixer/v1/config/fixed_cfg.pb.go

generate-mixer-v1-config-client-go: $(MIXER_CONFIG_CLIENT_PB_GOS) $(MIXER_CONFIG_DESCRIPTOR_PB_GOS)

generate-mixer-v1-template-go: $(MIXER_TEMPLATE_PB_GOS)

$(MIXER_V1_PB_GOS): $(MIXER_V1_PROTOS) | $(PROTOC_BIN)
	## Generate mixer/v1/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(MIXER_PLUGIN) $^

$(MIXER_CONFIG_PB_GOS) : $(MIXER_CONFIG_PROTOS) | $(PROTOC_BIN)
	## Generate mixer/v1/config/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(MIXER_PLUGIN) $^

$(MIXER_CONFIG_CLIENT_PB_GOS) : $(MIXER_CONFIG_CLIENT_PROTOS) | $(PROTOC_BIN)
	## Generate mixer/v1/config/client/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(MIXER_PLUGIN) $^

$(MIXER_CONFIG_DESCRIPTOR_PB_GOS) : $(MIXER_CONFIG_DESCRIPTOR_PROTOS) | $(PROTOC_BIN)
	## Generate mixer/v1/config/descriptor/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(MIXER_PLUGIN) $^

$(MIXER_TEMPLATE_PB_GOS) : $(MIXER_TEMPLATE_PROTOS) | $(PROTOC_BIN)
	## Generate mixer/v1/template/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(MIXER_PLUGIN) $^

mixer/v1/config/fixed_cfg.pb.go : mixer/v1/config/cfg.proto | $(PROTOC_BIN)
	# Generate mixer/v1/config/fixed_cfg.pb.go (requires alternate plugin and sed scripting due to issues with google.protobuf.Struct)
	$(PROTOC) $(PROTO_PATH) $(ALT_MIXER_PLUGIN) $^
	sed -e 's/*google_protobuf.Struct/interface{}/g' -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' mixer/v1/config/cfg.pb.go | goimports > mixer/v1/config/fixed_cfg.pb.go
	rm mixer/v1/config/cfg.pb.go

#####################
# broker/...
#####################

generate-broker-go: install-deps download-googleapis-protos protoc.version $(PROTOC_GEN_GO) $(BROKER_V1_PB_GOS)

$(BROKER_V1_PB_GOS): $(BROKER_V1_PROTOS) | $(PROTOC_BIN)
	## Generate broker/v1/config/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(STD_GO_PLUGIN) $^

#####################
# mesh/...
#####################

generate-mesh-go: install-deps download-googleapis-protos protoc.version $(PROTOC_GEN_GO) $(MESH_PB_GOS)

$(MESH_PB_GOS): $(MESH_PROTOS) | $(PROTOC_BIN)
	## Generate mesh/v1alpha1/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(STD_GO_PLUGIN) $^

#####################
# routing/...
#####################

generate-routing-go: install-deps download-googleapis-protos protoc.version $(PROTOC_GEN_GO) $(ROUTING_V1ALPHA1_PB_GOS) $(ROUTING_V1ALPHA2_PB_GOS)

$(ROUTING_V1ALPHA1_PB_GOS): $(ROUTING_V1ALPHA1_PROTOS) | $(PROTOC_BIN)
	## Generate routing/v1alpha1/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(STD_GO_PLUGIN) $^

$(ROUTING_V1ALPHA2_PB_GOS): $(ROUTING_V1ALPHA2_PROTOS) | $(PROTOC_BIN)
	## Generate routing/v1alpha2/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(STD_GO_PLUGIN) $^

#####################
# rbac/...
#####################

generate-rbac-go: install-deps download-googleapis-protos protoc.version $(PROTOC_GEN_GO) $(RBAC_V1_PB_GOS)

$(RBAC_V1_PB_GOS): $(RBAC_V1_PROTOS) | $(PROTOC_BIN)
	## Generate rbac/v1/*.pb.go
	$(PROTOC) $(PROTO_PATH) $(STD_GO_PLUGIN) $^

# TODO: kill all generated files too ?
clean:
	rm -rf bin/protoc-gen-*
	rm -rf bin/protoc-min-version-*
	rm -rf protoc-tmp
	rm -rf vendor
