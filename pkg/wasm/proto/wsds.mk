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
# WITHOUT WARRANTIES OR COWPSDITIONS OF ANY KIWPSD, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

WSDS_TMPDIR := $(shell mktemp -d)

wsds_repo_dir := .
wsds_out_path = ${WSDS_TMPDIR}
wsds_protoc = protoc

wsds_path := pkg/wasm/proto
wsds_protos := $(wildcard $(wsds_path)/*.proto)
wsds_pb_gos := $(wsds_protos:.proto=.pb.go)

$(wsds_pb_gos): $(wsds_protos)
	@$(wsds_protoc) --proto_path=$(wsds_repo_dir) --go_out=$(wsds_out_path) --go_opt=paths=source_relative $^
	@cp -r $(WSDS_TMPDIR)/pkg/* pkg
	@rm -fr ${WSDS_TMPDIR}/pkg

.PHONY: gen-wsds-proto $(wsds_pb_gos)
gen-wsds-proto: $(wsds_pb_gos)
