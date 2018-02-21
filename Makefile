all: generate

########################
# Version info
########################

std_go_version := $(shell bin/get_protoc_gen_version.sh golang)
gogo_version := $(shell bin/get_protoc_gen_version.sh gogo)
docs_version := master

########################
# protoc args
########################

proto_path := --proto_path=. --proto_path=vendor/github.com/gogo/protobuf --proto_path=vendor/istio.io/gogo-genproto/googleapis
out_path = :$(GOPATH)/src

########################
# protoc_gen_go
########################

protoc_gen_go_path := vendor/github.com/golang/protobuf
protoc_gen_go := genbin/protoc-gen-go-$(std_go_version)
protoc_gen_go_prefix := --plugin=$(protoc_gen_go) --go-$(std_go_version)_out=plugins=grpc,
protoc_gen_go_plugin := $(protoc_gen_go_prefix)$(out_path)

########################
# protoc_gen_gogo*
########################

protoc_gen_gogo_path := vendor/github.com/gogo/protobuf
protoc_gen_gogo := genbin/protoc-gen-gogo-$(gogo_version)
protoc_gen_gogoslick := genbin/protoc-gen-gogoslick-$(gogo_version)
protoc_min_version := genbin/protoc-min-version-$(gogo_version)

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
# protoc_gen_docs
########################

protoc_gen_docs_path := vendor/github.com/istio/tools
protoc_gen_docs := genbin/protoc-gen-docs-$(docs_version)
protoc_gen_docs_plugin := --plugin=$(protoc_gen_docs) --docs-$(docs_version)_out=warnings=true,mode=jekyll_html:

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

# BUGBUG: we override the use of protoc_min_version here, since using
#         that tool prevents warnings from protoc-gen-docs from being
#         displayed. If protoc_min_version gets fixed to allow this
#         data though, then remove this override
protoc := $(shell which protoc)

###################
# Deps
###################

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
	go build --pkgdir $(protoc_gen_gogo_path)/protoc-gen-gogo -o $(protoc_gen_gogo) ./$(protoc_gen_gogo_path)/protoc-gen-gogo

$(protoc_gen_gogoslick) : vendor
	@echo "Building protoc-gen-gogoslick..."
	go build --pkgdir $(protoc_gen_gogo_path)/protoc-gen-gogoslick -o $(protoc_gen_gogoslick) ./$(protoc_gen_gogo_path)/protoc-gen-gogoslick

$(protoc_gen_docs) : vendor
	@echo "Building protoc-gen-docs..."
	go build --pkgdir $(protoc_gen_docs_path)/protoc-gen-docs -o $(protoc_gen_docs) ./$(protoc_gen_docs_path)/protoc-gen-docs

$(protoc_min_version) : vendor
	@echo "Building protoc-min-version..."
	go build --pkgdir $(protoc_gen_gogo_path)/protoc-min-version -o $(protoc_min_version) ./$(protoc_gen_gogo_path)/protoc-min-version

binaries : $(protoc_gen_go) $(protoc_gen_gogo) $(protoc_gen_gogoslick) $(protoc_gen_docs) $(protoc_min_version)

depend: vendor binaries

#####################
# Generation Rules
#####################

generate: generate-broker-go generate-mesh-go generate-mixer-go generate-routing-go generate-rbac-go generate-authn-go

#####################
# broker/...
#####################

broker_v1_path := broker/dev
broker_v1_protos := $(shell find $(broker_v1_path) -type f -name '*.proto' | sort)
broker_v1_pb_gos := $(broker_v1_protos:.proto=.pb.go)
broker_v1_pb_doc := $(broker_v1_path)/istio.broker.dev.pb.html

generate-broker-go: $(broker_v1_pb_gos) $(broker_v1_pb_doc)

$(broker_v1_pb_gos) $(broker_v1_pb_doc): $(broker_v1_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate broker/dev/*.pb.go + $(broker_v1_pb_doc)
	@$(protoc) $(proto_path) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(broker_v1_path) $^

clean-broker-generated:
	rm -f $(broker_v1_pb_gos)
	rm -f $(broker_v1_pb_doc)

#####################
# mesh/...
#####################

mesh_path := mesh/v1alpha1
mesh_protos := $(shell find $(mesh_path) -type f -name '*.proto' | sort)
mesh_pb_gos := $(mesh_protos:.proto=.pb.go)
mesh_pb_doc := $(mesh_path)/istio.mesh.v1alpha1.pb.html

generate-mesh-go: $(mesh_pb_gos) $(mesh_pb_doc)

$(mesh_pb_gos) $(mesh_pb_doc): $(mesh_protos) | depend $(protoc_gen_GO) $(protoc_bin)
	## Generate mesh/v1alpha1/*.pb.go + $(mesh_pb_doc)
	@$(protoc) $(proto_path) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(mesh_path) $^

clean-mesh-generated:
	rm -f $(mesh_pb_gos)
	rm -f $(mesh_pb_doc)

#####################
# mixer/...
#####################

mixer_v1_path := mixer/v1
mixer_v1_protos :=  $(shell find $(mixer_v1_path) -maxdepth 1 -type f -name '*.proto' | sort)
mixer_v1_pb_gos := $(mixer_v1_protos:.proto=.pb.go)
mixer_v1_pb_doc := $(mixer_v1_path)/istio.mixer.v1.pb.html

mixer_config_client_path := mixer/v1/config/client
mixer_config_client_protos := $(shell find $(mixer_config_client_path) -maxdepth 1 -type f -name '*.proto' | sort)
mixer_config_client_pb_gos := $(mixer_config_client_protos:.proto=.pb.go)
mixer_config_client_pb_doc := $(mixer_config_client_path)/istio.mixer.v1.config.client.pb.html

mixer_config_descriptor_path := mixer/v1/config/descriptor
mixer_config_descriptor_protos := $(shell find $(mixer_config_descriptor_path) -maxdepth 1 -type f -name '*.proto' | sort)
mixer_config_descriptor_pb_gos := $(mixer_config_descriptor_protos:.proto=.pb.go)
mixer_config_descriptor_pb_doc := $(mixer_config_descriptor_path)/istio.mixer.v1.config.descriptor.pb.html

mixer_template_path := mixer/v1/template
mixer_template_protos := $(shell find $(mixer_template_path) -maxdepth 1 -type f -name '*.proto' | sort)
mixer_template_pb_gos := $(mixer_template_protos:.proto=.pb.go)
mixer_template_pb_doc := $(mixer_template_path)/istio.mixer.v1.template.pb.html

mixer_adapter_model_v1beta1_path := mixer/adapter/model/v1beta1
mixer_adapter_model_v1beta1_protos := $(shell find $(mixer_adapter_model_v1beta1_path) -maxdepth 1 -type f -name '*.proto' | sort)
mixer_adapter_model_v1beta1_pb_gos := $(mixer_adapter_model_v1beta1_protos:.proto=.pb.go)
mixer_adapter_model_v1beta1_pb_doc := $(mixer_adapter_model_v1beta1_path)/istio.mixer.adapter.model.v1beta1.pb.html

policy_v1beta1_path := policy/v1beta1
policy_v1beta1_protos := $(shell find $(policy_v1beta1_path) -maxdepth 1 -type f -name '*.proto' | sort)
policy_v1beta1_pb_gos := $(policy_v1beta1_protos:.proto=.pb.go)
policy_v1beta1_pb_doc := $(policy_v1beta1_path)/istio.policy.v1beta1.pb.html

generate-mixer-go: \
	$(mixer_v1_pb_gos) $(mixer_v1_pb_doc) \
	$(mixer_config_client_pb_gos) $(mixer_config_client_pb_doc) \
	$(mixer_config_descriptor_pb_gos) $(mixer_config_descriptor_pb_doc) \
	$(mixer_template_pb_gos) $(mixer_template_pb_doc) \
	$(mixer_adapter_model_v1beta1_pb_gos) $(mixer_adapter_model_v1beta1_pb_doc) \
	$(policy_v1beta1_pb_gos) $(policy_v1beta1_pb_doc) \
	mixer/v1/config/fixed_cfg.pb.go mixer/v1/config/istio.mixer.v1.config.pb.html

$(mixer_v1_pb_gos) $(mixer_v1_pb_doc): $(mixer_v1_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/v1/*.pb.go + $(mixer_v1_pb_doc)
	@$(protoc) $(proto_path) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_v1_path) $^

$(mixer_config_client_pb_gos) $(mixer_config_client_pb_doc): $(mixer_config_client_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/v1/config/client/*.pb.go + $(mixer_config_client_pb_doc)
	@$(protoc) $(proto_path) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_config_client_path) $^

$(mixer_config_descriptor_pb_gos) $(mixer_config_descriptor_pb_doc): $(mixer_config_descriptor_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/v1/config/descriptor/*.pb.go + $(mixer_config_descriptor_pb_doc)
	@$(protoc) $(proto_path) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_config_descriptor_path) $^

$(mixer_template_pb_gos) $(mixer_template_pb_doc) : $(mixer_template_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/v1/template/*.pb.go + $(mixer_template_pb_doc)
	@$(protoc) $(proto_path) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_template_path) $^

$(mixer_adapter_model_v1beta1_pb_gos) $(mixer_adapter_model_v1beta1_pb_doc) : $(mixer_adapter_model_v1beta1_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate mixer/adapter/model/v1beta1/*.pb.go + $(mixer_adapter_model_v1beta1_pb_doc)
	@$(protoc) $(proto_path) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_adapter_model_v1beta1_path) $^

$(policy_v1beta1_pb_gos) $(policy_v1beta1_pb_doc) : $(policy_v1beta1_protos) | depend $(protoc_gen_gogoslick) $(protoc_bin)
	## Generate policy/v1beta1/*.pb.go + $(policy_v1beta1_pb_doc)
	@$(protoc) $(proto_path) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(policy_v1beta1_path) $^
	## Generate policy/v1beta1/fixed_cfg.pb.go (requires alternate plugin and sed scripting due to issues with google.protobuf.Struct
	@$(protoc) $(proto_path) $(gogo_plugin) policy/v1beta1/cfg.proto
	@if [ -f "policy/v1beta1/cfg.pb.go" ]; then\
	    sed -e 's/*google_protobuf.Struct/interface{}/g' \
	        -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' \
	        -e 's/istio_policy_v1beta1\.//g' policy/v1beta1/cfg.pb.go \
	        | grep -v "google_protobuf" | grep -v "import istio_policy_v1beta1" >policy/v1beta1/fixed_cfg.pb.go;\
	    rm policy/v1beta1/cfg.pb.go;\
	fi

mixer/v1/config/fixed_cfg.pb.go mixer/v1/config/istio.mixer.v1.config.pb.html: mixer/v1/config/cfg.proto | depend $(protoc_gen_gogo) $(protoc_bin)
	# Generate mixer/v1/config/fixed_cfg.pb.go (requires alternate plugin and sed scripting due to issues with google.protobuf.Struct)
	@$(protoc) $(proto_path) $(gogo_plugin) $(protoc_gen_docs_plugin)mixer/v1/config $^
	@sed -e 's/*google_protobuf.Struct/interface{}/g' \
	     -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' mixer/v1/config/cfg.pb.go \
	     | grep -v "google_protobuf" >mixer/v1/config/fixed_cfg.pb.go
	@rm mixer/v1/config/cfg.pb.go

clean-mixer-generated:
	rm -f $(mixer_v1_pb_gos) $(mixer_config_client_pb_gos) $(mixer_config_descriptor_pb_gos) $(mixer_template_pb_gos) $(mixer_adapter_model_v1beta1_pb_gos) $(policy_v1beta1_pb_gos) policy/v1beta1/fixed_cfg.pb.go mixer/v1/config/fixed_cfg.pb.go
	rm -f $(mixer_v1_pb_doc) $(mixer_config_client_pb_doc) $(mixer_config_descriptor_pb_doc) $(mixer_template_pb_doc) $(mixer_adapter_model_v1beta1_pb_doc) $(policy_v1beta1_pb_doc) mixer/v1/config/istio.mixer.v1.config.pb.html

#####################
# routing/...
#####################

routing_v1alpha1_path := routing/v1alpha1
routing_v1alpha1_protos := $(shell find $(routing_v1alpha1_path) -type f -name '*.proto' | sort)
routing_v1alpha1_pb_gos := $(routing_v1alpha1_protos:.proto=.pb.go)
routing_v1alpha1_pb_doc := $(routing_v1alpha1_path)/istio.routing.v1alpha1.pb.html

routing_v1alpha2_path := routing/v1alpha2
routing_v1alpha2_protos := $(shell find routing/v1alpha2 -type f -name '*.proto' | sort)
routing_v1alpha2_pb_gos := $(routing_v1alpha2_protos:.proto=.pb.go)
routing_v1alpha2_pb_doc := $(routing_v1alpha2_path)/istio.routing.v1alpha2.pb.html

generate-routing-go: $(routing_v1alpha1_pb_gos) $(routing_v1alpha1_pb_doc) $(routing_v1alpha2_pb_gos) $(routing_v1alpha2_pb_doc)

$(routing_v1alpha1_pb_gos) $(routing_v1alpha1_pb_doc): $(routing_v1alpha1_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate routing/v1alpha1/*.pb.go +
	@$(protoc) $(proto_path) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(routing_v1alpha1_path) $^

$(routing_v1alpha2_pb_gos) $(routing_v1alpha2_pb_doc): $(routing_v1alpha2_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate routing/v1alpha2/*.pb.go
	@$(protoc) $(proto_path) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(routing_v1alpha2_path) $^

clean-routing-generated:
	rm -f $(routing_v1alpha1_pb_gos) $(routing_v1alpha2_pb_gos)
	rm -f $(routing_v1alpha1_pb_doc) $(routing_v1alpha2_pb_doc)

#####################
# rbac/...
#####################

rbac_v1alpha1_path := rbac/v1alpha1
rbac_v1alpha1_protos := $(shell find $(rbac_v1alpha1_path) -type f -name '*.proto' | sort)
rbac_v1alpha1_pb_gos := $(rbac_v1alpha1_protos:.proto=.pb.go)
rbac_v1alpha1_pb_doc := $(rbac_v1alpha1_path)/istio.rbac.v1alpha1.pb.html

generate-rbac-go: $(rbac_v1alpha1_pb_gos) $(rbac_v1alpha1_pb_doc)

$(rbac_v1alpha1_pb_gos) $(rbac_v1alpha1_pb_doc): $(rbac_v1alpha1_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate rbac/v1alpha1/*.pb.go
	@$(protoc) $(proto_path) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(rbac_v1alpha1_path) $^

clean-rbac-generated:
	rm -f $(rbac_v1alpha1_pb_gos)
	rm -f $(rbac_v1alpha1_pb_doc)


#####################
# authentication/...
#####################

authn_v1alpha1_path := authentication/v1alpha1
authn_v1alpha1_protos := $(shell find $(authn_v1alpha1_path) -type f -name '*.proto' | sort)
authn_v1alpha1_pb_gos := $(authn_v1alpha1_protos:.proto=.pb.go)
authn_v1alpha1_pb_doc := $(authn_v1alpha1_path)/istio.authentication.v1alpha1.pb.html

generate-authn-go: $(authn_v1alpha1_pb_gos) $(authn_v1alpha1_pb_doc)

$(authn_v1alpha1_pb_gos) $(authn_v1alpha1_pb_doc): $(authn_v1alpha1_protos) | depend $(protoc_gen_go) $(protoc_bin)
	## Generate authentication/v1alpha1/*.pb.go
	$(protoc) $(proto_path) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(authn_v1alpha1_path) $^

clean-authn-generated:
	rm -f $(authn_v1alpha1_pb_gos)
	rm -f $(authn_v1alpha1_pb_doc)


#####################
# Cleanup
#####################

clean:
	rm -rf genbin
	rm -rf vendor

clean-generated: clean-broker-generated clean-mesh-generated clean-mixer-generated clean-routing-generated clean-rbac-generated clean-authn-generated
