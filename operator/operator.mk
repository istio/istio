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

repo_dir := .
out_path = ${TMPDIR}
protoc = protoc -Icommon-protos -Ioperator

go_plugin_prefix := --go_out=plugins=grpc,
go_plugin := $(go_plugin_prefix):$(out_path)

python_output_path := operator/python/istio_api
protoc_gen_python_prefix := --python_out=,
protoc_gen_python_plugin := $(protoc_gen_python_prefix):$(repo_dir)/$(python_output_path)

protoc_gen_docs_plugin := --docs_out=warnings=true,mode=html_fragment_with_front_matter:$(repo_dir)/

v1alpha1_path := operator/pkg/apis/istio/v1alpha1
v1alpha1_protos := $(wildcard $(v1alpha1_path)/*.proto)
v1alpha1_pb_gos := $(v1alpha1_protos:.proto=.pb.go)
v1alpha1_pb_pythons := $(patsubst $(v1alpha1_path)/%.proto,$(python_output_path)/$(v1alpha1_path)/%_pb2.py,$(v1alpha1_protos))
v1alpha1_pb_docs := $(v1alpha1_path)/v1alpha1.pb.html
v1alpha1_openapi := $(v1alpha1_protos:.proto=.json)

$(v1alpha1_pb_gos) $(v1alpha1_pb_docs) $(v1alpha1_pb_pythons): $(v1alpha1_protos)
	@$(protoc) $(go_plugin) $(protoc_gen_docs_plugin)$(v1alpha1_path) $(protoc_gen_python_plugin) $^
	@cp -r ${TMPDIR}/pkg/* operator/pkg/
	@rm -fr ${TMPDIR}/pkg
	@go run $(repo_dir)/operator/pkg/apis/istio/fixup_structs/main.go -f $(v1alpha1_path)/values_types.pb.go

.PHONY: operator-proto
operator-proto: $(v1alpha1_pb_gos) $(v1alpha1_pb_docs) $(v1alpha1_pb_pythons)
