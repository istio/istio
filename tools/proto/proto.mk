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

.PHONY: proto operator-proto dns-proto

proto: operator-proto dns-proto echo-proto

operator-proto:
	buf generate --config tools/proto/buf.yaml --path operator/pkg/ --output operator  --template tools/proto/buf.gogo.yaml
	go run ./operator/pkg/apis/istio/fixup_structs/main.go -f operator/pkg/apis/istio/v1alpha1/values_types.pb.go

dns-proto:
	buf generate --config tools/proto/buf.yaml --path pkg/dns/ --output pkg  --template tools/proto/buf.golang.yaml

echo-proto:
	buf generate --config tools/proto/buf.yaml --path pkg/test/echo --output pkg  --template tools/proto/buf.golang.yaml
