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
docker_lock = protolock
docker_lock_release = ./scripts/check-release-locks.sh
docker_tool = prototool
else
gen_img := gcr.io/istio-testing/protoc:2019-03-29
lock_img := gcr.io/istio-testing/protolock:2019-03-11
all_img := gcr.io/istio-testing/api-build-tools:2019-03-11
pwd := $(shell pwd)
mount_dir := /src
repo_dir := istio.io/api
repo_mount := $(mount_dir)/istio.io/api
uid := $(shell id -u)
docker_gen := docker run --rm --user $(uid) -v /etc/passwd:/etc/passwd:ro -v $(pwd):$(repo_mount) -w $(mount_dir) $(gen_img) -I$(repo_dir)
out_path = .
docker_lock = docker run --rm --user $(uid) -v /etc/passwd:/etc/passwd:ro -v $(pwd):$(repo_mount) -w $(repo_mount) $(lock_img)
docker_lock_release = docker run --rm --user $(uid) -v /etc/passwd:/etc/passwd:ro -v $(pwd):$(repo_mount) -w $(repo_mount) --entrypoint=/bin/ash $(lock_img) $(repo_mount)/scripts/check-release-locks.sh
docker_tool = docker run --rm --user $(uid) -v /etc/passwd:/etc/passwd:ro -v $(pwd):$(repo_mount) -w $(repo_mount) $(all_img) prototool
endif

########################
# protoc_gen_gogo*
########################

gogo_plugin_prefix := --gogo_out=plugins=grpc,
gogofast_plugin_prefix := --gogofast_out=plugins=grpc,
gogoslick_plugin_prefix := --gogoslick_out=plugins=grpc,

########################
# protoc_gen_python
########################

protoc_gen_python_prefix := --python_out=,
protoc_gen_python_plugin := $(protoc_gen_python_prefix):$(repo_dir)/python/istio_api

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

gogo_plugin := $(gogo_plugin_prefix)$(gogo_mapping):$(out_path)
gogofast_plugin := $(gogofast_plugin_prefix)$(gogo_mapping):$(out_path)
gogoslick_plugin := $(gogoslick_plugin_prefix)$(gogo_mapping):$(out_path)

########################
# protoc_gen_docs
########################

protoc_gen_docs_plugin := --docs_out=warnings=true,dictionary=$(repo_dir)/dictionaries/en-US,custom_word_list=$(repo_dir)/dictionaries/custom.txt,mode=html_fragment_with_front_matter:$(repo_dir)/
protoc_gen_docs_plugin_for_networking := --docs_out=warnings=true,dictionary=$(repo_dir)/dictionaries/en-US,custom_word_list=$(repo_dir)/dictionaries/custom.txt,per_file=true,mode=html_fragment_with_front_matter:$(repo_dir)/

#####################
# Generation Rules
#####################

generate: \
	generate-mcp-go \
	generate-mcp-python \
	generate-mesh-go \
	generate-mesh-python \
	generate-mixer-go \
	generate-mixer-python \
	generate-routing-go \
	generate-routing-python \
	generate-rbac-go \
	generate-rbac-python \
	generate-authn-go \
	generate-authn-python \
	generate-envoy-go \
	generate-envoy-python

#####################
# mcp/...
#####################

config_mcp_path := mcp/v1alpha1
config_mcp_protos := $(shell find $(config_mcp_path) -type f -name '*.proto' | sort)
config_mcp_pb_gos := $(config_mcp_protos:.proto=.pb.go)
config_mcp_pb_pythons := $(config_mcp_protos:.proto=_pb2.py)
config_mcp_pb_doc := $(config_mcp_path)/istio.mcp.v1alpha1.pb.html

generate-mcp-go: $(config_mcp_pb_gos) $(config_mcp_pb_doc)

$(config_mcp_pb_gos) $(config_mcp_pb_doc): $(config_mcp_protos)
	## Generate mcp/v1alpha1/*.pb.go + $(config_mcp_pb_doc)
	@$(docker_lock) status
	@$(docker_gen) $(gogofast_plugin) $(protoc_gen_docs_plugin)$(config_mcp_path) $^

generate-mcp-python: $(config_mcp_pb_pythons)

$(config_mcp_pb_pythons): $(config_mcp_protos)
	## Generate python/istio_api/mcp/v1alpha1/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

clean-mcp:
	rm -f $(config_mcp_pb_gos)
	rm -f $(config_mcp_pb_doc)

#####################
# mesh/...
#####################

mesh_path := mesh/v1alpha1
mesh_protos := $(shell find $(mesh_path) -type f -name '*.proto' | sort)
mesh_pb_gos := $(mesh_protos:.proto=.pb.go)
mesh_pb_pythons := $(mesh_protos:.proto=_pb2.py)
mesh_pb_doc := $(mesh_path)/istio.mesh.v1alpha1.pb.html

generate-mesh-go: $(mesh_pb_gos) $(mesh_pb_doc)

$(mesh_pb_gos) $(mesh_pb_doc): $(mesh_protos)
	## Generate mesh/v1alpha1/*.pb.go + $(mesh_pb_doc)
	@$(docker_lock) status
	@$(docker_gen) $(gogofast_plugin) $(protoc_gen_docs_plugin)$(mesh_path) $^

generate-mesh-python: $(mesh_pb_pythons)

$(mesh_pb_pythons): $(mesh_protos)
	## Generate python/istio_api/mesh/v1alpha1/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

clean-mesh:
	rm -f $(mesh_pb_gos)
	rm -f $(mesh_pb_doc)

#####################
# mixer/...
#####################

mixer_v1_path := mixer/v1
mixer_v1_protos :=  $(shell find $(mixer_v1_path) -maxdepth 1 -type f -name '*.proto' | sort)
mixer_v1_pb_gos := $(mixer_v1_protos:.proto=.pb.go)
mixer_v1_pb_pythons := $(mixer_v1_protos:.proto=_pb2.py)
mixer_v1_pb_doc := $(mixer_v1_path)/istio.mixer.v1.pb.html

mixer_config_client_path := mixer/v1/config/client
mixer_config_client_protos := $(shell find $(mixer_config_client_path) -maxdepth 1 -type f -name '*.proto' | sort)
mixer_config_client_pb_gos := $(mixer_config_client_protos:.proto=.pb.go)
mixer_config_client_pb_pythons := $(mixer_config_client_protos:.proto=_pb2.py)
mixer_config_client_pb_doc := $(mixer_config_client_path)/istio.mixer.v1.config.client.pb.html

mixer_adapter_model_v1beta1_path := mixer/adapter/model/v1beta1
mixer_adapter_model_v1beta1_protos := $(shell find $(mixer_adapter_model_v1beta1_path) -maxdepth 1 -type f -name '*.proto' | sort)
mixer_adapter_model_v1beta1_pb_gos := $(mixer_adapter_model_v1beta1_protos:.proto=.pb.go)
mixer_adapter_model_v1beta1_pb_pythons := $(mixer_adapter_model_v1beta1_protos:.proto=_pb2.py)
mixer_adapter_model_v1beta1_pb_doc := $(mixer_adapter_model_v1beta1_path)/istio.mixer.adapter.model.v1beta1.pb.html

policy_v1beta1_path := policy/v1beta1
policy_v1beta1_protos := $(shell find $(policy_v1beta1_path) -maxdepth 1 -type f -name '*.proto' | sort)
policy_v1beta1_pb_gos := $(policy_v1beta1_protos:.proto=.pb.go)
policy_v1beta1_pb_pythons := $(policy_v1beta1_protos:.proto=_pb2.py)
policy_v1beta1_pb_doc := $(policy_v1beta1_path)/istio.policy.v1beta1.pb.html

generate-mixer-go: \
	$(mixer_v1_pb_gos) $(mixer_v1_pb_doc) \
	$(mixer_config_client_pb_gos) $(mixer_config_client_pb_doc) \
	$(mixer_adapter_model_v1beta1_pb_gos) $(mixer_adapter_model_v1beta1_pb_doc) \
	$(policy_v1beta1_pb_gos) $(policy_v1beta1_pb_doc)

$(mixer_v1_pb_gos) $(mixer_v1_pb_doc): $(mixer_v1_protos)
	## Generate mixer/v1/*.pb.go + $(mixer_v1_pb_doc)
	@$(docker_lock) status
	@$(docker_gen) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_v1_path) $^

$(mixer_config_client_pb_gos) $(mixer_config_client_pb_doc): $(mixer_config_client_protos)
	## Generate mixer/v1/config/client/*.pb.go + $(mixer_config_client_pb_doc)
	@$(docker_lock) status
	@$(docker_gen) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_config_client_path) $^

$(mixer_adapter_model_v1beta1_pb_gos) $(mixer_adapter_model_v1beta1_pb_doc) : $(mixer_adapter_model_v1beta1_protos)
	## Generate mixer/adapter/model/v1beta1/*.pb.go + $(mixer_adapter_model_v1beta1_pb_doc)
	@$(docker_lock) status
	@$(docker_gen) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(mixer_adapter_model_v1beta1_path) $^

$(policy_v1beta1_pb_gos) $(policy_v1beta1_pb_doc) : $(policy_v1beta1_protos)
	## Generate policy/v1beta1/*.pb.go + $(policy_v1beta1_pb_doc)
	@$(docker_lock) status
	@$(docker_gen) $(gogoslick_plugin) $(protoc_gen_docs_plugin)$(policy_v1beta1_path) $^

generate-mixer-python: \
	$(mixer_v1_pb_pythons) \
	$(mixer_config_client_pb_pythons) \
	$(mixer_adapter_model_v1beta1_pb_pythons) \
	$(policy_v1beta1_pb_pythons)

$(mixer_v1_pb_pythons): $(mixer_v1_protos)
	## Generate python/istio_api/mixer/v1/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

$(mixer_config_client_pb_pythons): $(mixer_config_client_protos)
	## Generate python/istio_api/mixer/v1/config/client/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

$(mixer_adapter_model_v1beta1_pb_pythons): $(mixer_adapter_model_v1beta1_protos)
	## Generate python/istio_api/mixer/adapter/model/v1beta1/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

$(policy_v1beta1_pb_pythons): $(policy_v1beta1_protos)
	## Generate python/istio_api/policy/v1beta1/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

clean-mixer:
	rm -f $(mixer_v1_pb_gos) $(mixer_config_client_pb_gos) $(mixer_adapter_model_v1beta1_pb_gos) $(policy_v1beta1_pb_gos) policy/v1beta1/fixed_cfg.pb.go
	rm -f $(mixer_v1_pb_doc) $(mixer_config_client_pb_doc) $(mixer_adapter_model_v1beta1_pb_doc) $(policy_v1beta1_pb_doc)

#####################
# routing/...
#####################

routing_v1alpha3_path := networking/v1alpha3
routing_v1alpha3_protos := $(shell find networking/v1alpha3 -type f -name '*.proto' | sort)
routing_v1alpha3_pb_gos := $(routing_v1alpha3_protos:.proto=.pb.go)
routing_v1alpha3_pb_pythons := $(routing_v1alpha3_protos:.proto=_pb2.py)
routing_v1alpha3_pb_docs := $(routing_v1alpha3_protos:.proto=.pb.html)

generate-routing-go: $(routing_v1alpha3_pb_gos) $(routing_v1alpha3_pb_docs)

$(routing_v1alpha3_pb_gos) $(routing_v1alpha3_pb_docs): $(routing_v1alpha3_protos)
	## Generate networking/v1alpha3/*.pb.go
	@$(docker_lock) status
	@$(docker_gen) $(gogofast_plugin) $(protoc_gen_docs_plugin_for_networking)$(routing_v1alpha3_path) $^

generate-routing-python: $(routing_v1alpha3_pb_pythons)

$(routing_v1alpha3_pb_pythons): $(routing_v1alpha3_protos)
	## Generate python/istio_api/networking/v1alpha3/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

clean-routing:
	rm -f $(routing_v1alpha3_pb_gos)
	rm -f $(routing_v1alpha3_pb_docs)

#####################
# rbac/...
#####################

rbac_v1alpha1_path := rbac/v1alpha1
rbac_v1alpha1_protos := $(shell find $(rbac_v1alpha1_path) -type f -name '*.proto' | sort)
rbac_v1alpha1_pb_gos := $(rbac_v1alpha1_protos:.proto=.pb.go)
rbac_v1alpha1_pb_pythons := $(rbac_v1alpha1_protos:.proto=_pb2.py)
rbac_v1alpha1_pb_doc := $(rbac_v1alpha1_path)/istio.rbac.v1alpha1.pb.html

generate-rbac-go: $(rbac_v1alpha1_pb_gos) $(rbac_v1alpha1_pb_doc)

$(rbac_v1alpha1_pb_gos) $(rbac_v1alpha1_pb_doc): $(rbac_v1alpha1_protos)
	## Generate rbac/v1alpha1/*.pb.go
	@$(docker_lock) status
	@$(docker_gen) $(gogofast_plugin) $(protoc_gen_docs_plugin)$(rbac_v1alpha1_path) $^

generate-rbac-python: $(rbac_v1alpha1_protos)

$(rbac_v1alpha1_pb_pythons): $(rbac_v1alpha1_protos)
	## Generate python/istio_api/rbac/v1alpha1/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

clean-rbac:
	rm -f $(rbac_v1alpha1_pb_gos)
	rm -f $(rbac_v1alpha1_pb_doc)


#####################
# authentication/...
#####################

authn_v1alpha1_path := authentication/v1alpha1
authn_v1alpha1_protos := $(shell find $(authn_v1alpha1_path) -type f -name '*.proto' | sort)
authn_v1alpha1_pb_gos := $(authn_v1alpha1_protos:.proto=.pb.go)
authn_v1alpha1_pb_pythons := $(authn_v1alpha1_protos:.proto=_pb2.py)
authn_v1alpha1_pb_doc := $(authn_v1alpha1_path)/istio.authentication.v1alpha1.pb.html

generate-authn-go: $(authn_v1alpha1_pb_gos) $(authn_v1alpha1_pb_doc)

$(authn_v1alpha1_pb_gos) $(authn_v1alpha1_pb_doc): $(authn_v1alpha1_protos)
	## Generate authentication/v1alpha1/*.pb.go
	@$(docker_lock) status
	@$(docker_gen) $(gogofast_plugin) $(protoc_gen_docs_plugin)$(authn_v1alpha1_path) $^

generate-authn-python: $(authn_v1alpha1_pb_pythons)

$(authn_v1alpha1_pb_pythons): $(authn_v1alpha1_protos)
	## Generate python/istio_api/authentication/v1alpha1/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

clean-authn:
	rm -f $(authn_v1alpha1_pb_gos)
	rm -f $(authn_v1alpha1_pb_doc)

#####################
# envoy/...
#####################

envoy_path := envoy
envoy_protos := $(shell find $(envoy_path) -type f -name '*.proto' | sort)
envoy_pb_gos := $(envoy_protos:.proto=.pb.go)
envoy_pb_pythons := $(envoy_protos:.proto=_pb2.py)

generate-envoy-go: $(envoy_pb_gos) $(envoy_pb_doc)

# Envoy APIs is internal APIs, documents is not required.
$(envoy_pb_gos): %.pb.go : %.proto
	## Generate envoy/*/*.pb.go
	@$(docker_lock) status
	@$(docker_gen) $(gogofast_plugin) $<

generate-envoy-python: $(envoy_pb_pythons)

# Envoy APIs is internal APIs, documents is not required.
$(envoy_pb_pythons): $(envoy_protos)
	## Generate envoy/*/*_pb2.py
	@$(docker_lock) status
	@$(docker_gen) $(protoc_gen_python_plugin) $^

clean-envoy:
	rm -f $(envoy_pb_gos)

#####################
# Protolock
#####################

proto-commit:
	@$(docker_lock) commit

proto-commit-force:
	@$(docker_lock) commit --force

proto-status:
	@$(docker_lock) status

release-lock-status:
	@$(docker_lock_release)

#####################
# Lint
#####################

lint:
	@$(docker_tool) lint --protoc-bin-path=/usr/bin/protoc --protoc-wkt-path=/protobuf

#####################
# Cleanup
#####################

clean-python:
	rm -rf python/istio_api/*

clean: 	clean-mcp \
	clean-mesh \
	clean-mixer \
	clean-routing \
	clean-rbac \
	clean-authn \
	clean-envoy \
	clean-python
