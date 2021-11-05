// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha3

import (
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	dnsProto "istio.io/istio/pkg/dns/proto"
	dnsServer "istio.io/istio/pkg/dns/server"
)

// BuildNameTable produces a table of hostnames and their associated IPs that can then
// be used by the agent to resolve DNS. This logic is always active. However, local DNS resolution
// will only be effective if DNS capture is enabled in the proxy
func (configgen *ConfigGeneratorImpl) BuildNameTable(node *model.Proxy, push *model.PushContext) *dnsProto.NameTable {
	return dnsServer.BuildNameTable(dnsServer.Config{
		Node:                        node,
		Push:                        push,
		MulticlusterHeadlessEnabled: features.MulticlusterHeadlessEnabled,
	})
}
