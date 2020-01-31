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

pwd := $(shell pwd)
TMPDIR := $(shell mktemp -d)

.PHONY: icp-proto operator-proto operator-clean

repo_dir := .
out_path = ${TMPDIR}
protoc = protoc -Icommon-protos -Ioperator

go_plugin_prefix := --go_out=plugins=grpc,
go_plugin := $(go_plugin_prefix):$(out_path)

########################
# protoc_gen_python
########################

python_output_path := operator/python/operator_api
protoc_gen_python_prefix := --python_out=,
protoc_gen_python_plugin := $(protoc_gen_python_prefix):$(repo_dir)/$(python_output_path)

########################
# protoc_gen_docs
########################

protoc_gen_docs_plugin := --docs_out=warnings=true,dictionary=$(repo_dir)/dictionaries/en-US,custom_word_list=$(repo_dir)/dictionaries/custom.txt,mode=html_fragment_with_front_matter:$(repo_dir)/
protoc_gen_docs_plugin_per_file := --docs_out=warnings=true,dictionary=$(repo_dir)/dictionaries/en-US,custom_word_list=$(repo_dir)/dictionaries/custom.txt,per_file=true,mode=html_fragment_with_front_matter:$(repo_dir)/

########################

# Legacy IstioControlPlane included for translation purposes.
icp_v1alpha2_path := operator/pkg/apis/istio/v1alpha2
icp_v1alpha2_protos := $(wildcard $(icp_v1alpha2_path)/*.proto)
icp_v1alpha2_pb_gos := $(icp_v1alpha2_protos:.proto=.pb.go)
icp_v1alpha2_pb_pythons := $(patsubst $(icp_v1alpha2_path)/%.proto,$(python_output_path)/$(icp_v1alpha2_path)/%_pb2.py,$(icp_v1alpha2_protos))
icp_v1alpha2_pb_docs := $(icp_v1alpha2_path)/v1alpha2.pb.html
icp_v1alpha2_openapi := $(icp_v1alpha2_protos:.proto=.json)

$(icp_v1alpha2_pb_gos) $(icp_v1alpha2_pb_docs) $(icp_v1alpha2_pb_pythons): $(icp_v1alpha2_protos)
	@$(protoc) $(go_plugin) $(protoc_gen_docs_plugin)$(icp_v1alpha2_path) $(protoc_gen_python_plugin) $^
	cp -r ${TMPDIR}/pkg/* operator/pkg/
	rm -fr ${TMPDIR}/pkg
	go run $(repo_dir)/operator/pkg/apis/istio/fixup_structs/main.go -f $(icp_v1alpha2_path)/istiocontrolplane_types.pb.go
	@sed -i 's|<key,value,effect>|\&lt\;key,value,effect\&gt\;|g' $(icp_v1alpha2_path)/v1alpha2.pb.html
	@sed -i 's|<operator>|\&lt\;operator\&gt\;|g' $(icp_v1alpha2_path)/v1alpha2.pb.html

v1alpha1_path := operator/pkg/apis/istio/v1alpha1
v1alpha1_protos := $(wildcard $(v1alpha1_path)/*.proto)
v1alpha1_pb_gos := $(v1alpha1_protos:.proto=.pb.go)
v1alpha1_pb_pythons := $(patsubst $(v1alpha1_path)/%.proto,$(python_output_path)/$(v1alpha1_path)/%_pb2.py,$(v1alpha1_protos))
v1alpha1_pb_docs := $(v1alpha1_path)/v1alpha1.pb.html
v1alpha1_openapi := $(v1alpha1_protos:.proto=.json)

$(v1alpha1_pb_gos) $(v1alpha1_pb_docs) $(v1alpha1_pb_pythons): $(v1alpha1_protos)
	$(protoc) $(go_plugin) $(protoc_gen_docs_plugin)$(v1alpha1_path) $(protoc_gen_python_plugin) $^
	cp -r ${TMPDIR}/pkg/* operator/pkg/
	rm -fr ${TMPDIR}/pkg
	go run $(repo_dir)/operator/pkg/apis/istio/fixup_structs/main.go -f $(v1alpha1_path)/values_types.pb.go

icp-proto: $(icp_v1alpha2_pb_gos) $(icp_v1alpha2_pb_docs) $(icp_v1alpha2_pb_pythons)

operator-proto: $(v1alpha1_pb_gos) $(v1alpha1_pb_docs) $(v1alpha1_pb_pythons)

operator-clean:
	@rm -fr $(icp_v1alpha2_pb_gos) $(icp_v1alpha2_pb_docs) $(icp_v1alpha2_pb_pythons)
	@rm -fr $(1alpha1_pb_gos) $(v1alpha1_pb_docs) $(v1alpha1_pb_pythons)
