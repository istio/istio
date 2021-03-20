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

package options

import (
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/model"
)

func NewStatusServerOptions(proxy *model.Proxy, proxyConfig *meshconfig.ProxyConfig) *status.Options {
	return &status.Options{
		IPv6:           IsIPv6Proxy(proxy.IPAddresses),
		PodIP:          InstanceIPVar.Get(),
		AdminPort:      uint16(proxyConfig.ProxyAdminPort),
		StatusPort:     uint16(proxyConfig.StatusPort),
		KubeAppProbers: kubeAppProberNameVar.Get(),
		NodeType:       proxy.Type,
	}
}
