# Copyright Istio Authors
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

NDS_TMPDIR := $(shell mktemp -d)

nds_repo_dir := .
nds_out_path = ${NDS_TMPDIR}
nds_protoc = protoc

nds_path := pkg/dns/proto
nds_protos := $(wildcard $(nds_path)/*.proto)
nds_pb_gos := $(nds_protos:.proto=.pb.go)

$(nds_pb_gos): $(nds_protos)
	@$(nds_protoc) --proto_path=$(nds_repo_dir) --go_out=$(nds_out_path) --go_opt=paths=source_relative $^
	@cp -r $(NDS_TMPDIR)/pkg/* pkg
	@rm -fr ${NDS_TMPDIR}/pkg

.PHONY: gen-nds-proto $(nds_pb_gos)
gen-nds-proto: $(nds_pb_gos)
