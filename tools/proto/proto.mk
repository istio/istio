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

BUF_CONFIG_DIR := tools/proto

.PHONY: proto operator-proto dns-proto

proto: operator-proto dns-proto echo-proto workload-proto zds-proto

operator-proto:
	buf generate --config $(BUF_CONFIG_DIR)/buf.yaml --path operator/pkg/ --output operator --template $(BUF_CONFIG_DIR)/buf.golang.yaml

dns-proto:
	buf generate --config $(BUF_CONFIG_DIR)/buf.yaml --path pkg/dns/ --output pkg --template $(BUF_CONFIG_DIR)/buf.golang.yaml

echo-proto:
	buf generate --config $(BUF_CONFIG_DIR)/buf.yaml --path pkg/test/echo --output pkg --template $(BUF_CONFIG_DIR)/buf.golang.yaml

workload-proto:
	buf generate --config $(BUF_CONFIG_DIR)/buf.yaml --path pkg/workloadapi --output pkg --template $(BUF_CONFIG_DIR)/buf.golang-json.yaml

zds-proto:
	buf generate --config $(BUF_CONFIG_DIR)/buf.yaml --path pkg/zdsapi --output pkg --template $(BUF_CONFIG_DIR)/buf.golang.yaml
