all: generate

########################
# docker_gen
########################

# Use a different generation mechanism when running from the
# image itself
ifdef CIRCLECI
repo_dir = .
docker_gen = /usr/bin/protoc -I/protobuf -I$(repo_dir)
out_path = $(OUT_PATH)
else
gen_img := gcr.io/istio-testing/protoc:2018-03-03
pwd := $(shell pwd)
mount_dir := /src
repo_dir := istio.io/api
repo_mount := $(mount_dir)/istio.io/api
docker_gen := docker run --rm -v $(pwd):$(repo_mount) -w $(mount_dir) $(gen_img) -I$(repo_dir)
out_path = .
endif


########################
# protoc_gen_go
########################

protoc_gen_go_prefix := --go_out=plugins=grpc,
protoc_gen_go_plugin := $(protoc_gen_go_prefix):$(out_path)

########################
# protoc_gen_gogo*
########################

gogo_plugin_prefix := --gogo_out=plugins=grpc,
gogofast_plugin_prefix := --gogofast_out=plugins=grpc,
gogoslick_plugin_prefix := --gogoslick_out=plugins=grpc,

comma := ,
empty:=
space := $(empty) $(empty)

importmaps := \
	gogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto \
	google/protobuf/any.proto=github.com/gogo/protobuf/types \
	google/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor \
	google/protobuf/duration.proto=github.com/gogo/protobuf/types \
	google/protobuf/timestamp.proto=github.com/gogo/protobuf/types \
	google/protobuf/wrappers.proto=github.com/gogo/protobuf/types \
	google/rpc/status.proto=github.com/gogo/googleapis/google/rpc \
	google/rpc/code.proto=github.com/gogo/googleapis/google/rpc \
	google/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc \

# generate mapping directive with M<proto>:<go pkg>, format for each proto file
mapping_with_spaces := $(foreach map,$(importmaps),M$(map),)
gogo_mapping := $(subst $(space),$(empty),$(mapping_with_spaces))

gogo_plugin := $(gogo_plugin_prefix)$(gogo_mapping):$(out_path)
gogofast_plugin := $(gogofast_plugin_prefix)$(gogo_mapping):$(out_path)
gogoslick_plugin := $(gogoslick_plugin_prefix)$(gogo_mapping):$(out_path)

########################
# protoc_gen_docs
########################

protoc_gen_docs_plugin := --docs_out=warnings=true,mode=jekyll_html:$(repo_dir)/

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

$(broker_v1_pb_gos) $(broker_v1_pb_doc): $(broker_v1_protos)
	## Generate broker/dev/*.pb.go + $(broker_v1_pb_doc)
	@$(docker_gen) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(broker_v1_path) $^

clean-broker:
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

$(mesh_pb_gos) $(mesh_pb_doc): $(mesh_protos)
	## Generate mesh/v1alpha1/*.pb.go + $(mesh_pb_doc)
	@$(docker_gen) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(mesh_path) $^

clean-mesh:
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
	$(mixer_adapter_model_v1beta1_pb_gos) $(mixer_adapter_model_v1beta1_pb_doc) \
	$(policy_v1beta1_pb_gos) $(policy_v1beta1_pb_doc)

$(mixer_v1_pb_gos) $(mixer_v1_pb_doc): $(mixer_v1_protos)
	## Generate mixer/v1/*.pb.go + $(mixer_v1_pb_doc)
	@$(docker_gen) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_v1_path) $^

$(mixer_config_client_pb_gos) $(mixer_config_client_pb_doc): $(mixer_config_client_protos)
	## Generate mixer/v1/config/client/*.pb.go + $(mixer_config_client_pb_doc)
	@$(docker_gen) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_config_client_path) $^

$(mixer_adapter_model_v1beta1_pb_gos) $(mixer_adapter_model_v1beta1_pb_doc) : $(mixer_adapter_model_v1beta1_protos)
	## Generate mixer/adapter/model/v1beta1/*.pb.go + $(mixer_adapter_model_v1beta1_pb_doc)
	@$(docker_gen) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_adapter_model_v1beta1_path) $^

$(policy_v1beta1_pb_gos) $(policy_v1beta1_pb_doc) : $(policy_v1beta1_protos)
	## Generate policy/v1beta1/*.pb.go + $(policy_v1beta1_pb_doc)
	@$(docker_gen) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(policy_v1beta1_path) $^
	## Generate policy/v1beta1/fixed_cfg.pb.go (requires alternate plugin and sed scripting due to issues with google.protobuf.Struct
	@$(docker_gen) $(gogo_plugin) policy/v1beta1/cfg.proto
	@if [ -f "policy/v1beta1/cfg.pb.go" ]; then\
		sed -e 's/*google_protobuf.Struct/interface{}/g' \
			-e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' \
			-e 's/istio_policy_v1beta1\.//g' policy/v1beta1/cfg.pb.go \
			| grep -v "google_protobuf" | grep -v "import istio_policy_v1beta1" >policy/v1beta1/fixed_cfg.pb.go;\
		rm -f policy/v1beta1/cfg.pb.go;\
	fi

clean-mixer:
	rm -f $(mixer_v1_pb_gos) $(mixer_config_client_pb_gos) $(mixer_adapter_model_v1beta1_pb_gos) $(policy_v1beta1_pb_gos) policy/v1beta1/fixed_cfg.pb.go
	rm -f $(mixer_v1_pb_doc) $(mixer_config_client_pb_doc) $(mixer_adapter_model_v1beta1_pb_doc) $(policy_v1beta1_pb_doc)

#####################
# routing/...
#####################

routing_v1alpha1_path := routing/v1alpha1
routing_v1alpha1_protos := $(shell find $(routing_v1alpha1_path) -type f -name '*.proto' | sort)
routing_v1alpha1_pb_gos := $(routing_v1alpha1_protos:.proto=.pb.go)
routing_v1alpha1_pb_doc := $(routing_v1alpha1_path)/istio.routing.v1alpha1.pb.html

routing_v1alpha3_path := networking/v1alpha3
routing_v1alpha3_protos := $(shell find networking/v1alpha3 -type f -name '*.proto' | sort)
routing_v1alpha3_pb_gos := $(routing_v1alpha3_protos:.proto=.pb.go)
routing_v1alpha3_pb_doc := $(routing_v1alpha3_path)/istio.routing.v1alpha3.pb.html

generate-routing-go: $(routing_v1alpha1_pb_gos) $(routing_v1alpha1_pb_doc) $(routing_v1alpha3_pb_gos) $(routing_v1alpha3_pb_doc)

$(routing_v1alpha1_pb_gos) $(routing_v1alpha1_pb_doc): $(routing_v1alpha1_protos)
	## Generate routing/v1alpha1/*.pb.go +
	@$(docker_gen) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(routing_v1alpha1_path) $^

$(routing_v1alpha3_pb_gos) $(routing_v1alpha3_pb_doc): $(routing_v1alpha3_protos)
	## Generate networking/v1alpha3/*.pb.go
	@$(docker_gen) $(gogofast_plugin) $(protoc_gen_docs_plugin)$(routing_v1alpha3_path) $^

clean-routing:
	rm -f $(routing_v1alpha1_pb_gos) $(routing_v1alpha3_pb_gos)
	rm -f $(routing_v1alpha1_pb_doc) $(routing_v1alpha3_pb_doc)

#####################
# rbac/...
#####################

rbac_v1alpha1_path := rbac/v1alpha1
rbac_v1alpha1_protos := $(shell find $(rbac_v1alpha1_path) -type f -name '*.proto' | sort)
rbac_v1alpha1_pb_gos := $(rbac_v1alpha1_protos:.proto=.pb.go)
rbac_v1alpha1_pb_doc := $(rbac_v1alpha1_path)/istio.rbac.v1alpha1.pb.html

generate-rbac-go: $(rbac_v1alpha1_pb_gos) $(rbac_v1alpha1_pb_doc)

$(rbac_v1alpha1_pb_gos) $(rbac_v1alpha1_pb_doc): $(rbac_v1alpha1_protos)
	## Generate rbac/v1alpha1/*.pb.go
	@$(docker_gen) $(protoc_gen_go_plugin) $(protoc_gen_docs_plugin)$(rbac_v1alpha1_path) $^

clean-rbac:
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

$(authn_v1alpha1_pb_gos) $(authn_v1alpha1_pb_doc): $(authn_v1alpha1_protos)
	## Generate authentication/v1alpha1/*.pb.go
	@$(docker_gen) $(gogofast_plugin) $(protoc_gen_docs_plugin)$(authn_v1alpha1_path) $^

clean-authn:
	rm -f $(authn_v1alpha1_pb_gos)
	rm -f $(authn_v1alpha1_pb_doc)


#####################
# Cleanup
#####################

clean: clean-broker clean-mesh clean-mixer clean-routing clean-rbac clean-authn
