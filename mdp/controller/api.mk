pwd_mdp := $(shell pwd)

TMPDIR_mdp := $(shell mktemp -d)

repo_dir_mdp := .
out_path_mdp = ${TMPDIR_mdp}
protoc_mdp = protoc -Icommon-protos -Imdp/controller

go_plugin_prefix_mdp := --gogofast_out=plugins=grpc,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types
go_plugin_mdp := $(go_plugin_prefix_mdp):$(out_path_mdp)

v1alpha1_path_mdp := mdp/controller/pkg/apis/mdp/v1alpha1
v1alpha1_protos_mdp := $(wildcard $(v1alpha1_path_mdp)/*.proto)
v1alpha1_pb_gos_mdp := $(v1alpha1_protos_mdp:.proto=.pb.go)
v1alpha1_openapi_mdp := $(v1alpha1_protos_mdp:.proto=.json)

$(v1alpha1_pb_gos_mdp): $(v1alpha1_protos_mdp)
	@$(protoc_mdp) $(go_plugin_mdp) $^
	@cp -r ${TMPDIR_mdp}/pkg/* mdp/controller/pkg/
	@rm -fr ${TMPDIR_mdp}/pkg

.PHONY: mdp-proto v1alpha1_pb_gos_mdp
mdp-proto: $(v1alpha1_pb_gos_mdp)
