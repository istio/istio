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

package xds

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
)

// configKindAffectedProxyTypes contains known config types which may affect certain node types.
var configKindAffectedProxyTypes = map[kind.Kind][]model.NodeType{
	kind.Gateway: {model.Router},
	kind.Sidecar: {model.SidecarProxy},
}

// ConfigAffectsProxy checks if a pushEv will affect a specified proxy. That means whether the push will be performed
// towards the proxy.
func ConfigAffectsProxy(req *model.PushRequest, proxy *model.Proxy) bool {
	// Empty changes means "all" to get a backward compatibility.
	if len(req.ConfigsUpdated) == 0 {
		return true
	}

	for config := range req.ConfigsUpdated {
		affected := true

		// Some configKinds only affect specific proxy types
		if kindAffectedTypes, f := configKindAffectedProxyTypes[config.Kind]; f {
			affected = false
			for _, t := range kindAffectedTypes {
				if t == proxy.Type {
					affected = true
					break
				}
			}
		}

		if affected && checkProxyDependencies(proxy, config, req.Push) {
			return true
		}
	}

	return false
}

func checkProxyDependencies(proxy *model.Proxy, config model.ConfigKey, push *model.PushContext) bool {
	// Detailed config dependencies check.
	switch proxy.Type {
	case model.SidecarProxy:
		if proxy.SidecarScope.DependsOnConfig(config) {
			return true
		} else if proxy.PrevSidecarScope != nil && proxy.PrevSidecarScope.DependsOnConfig(config) {
			return true
		}
	case model.Router:
		if config.Kind == kind.ServiceEntry {
			// If config is ServiceEntry, name of the config is service's FQDN
			svc, exist := push.ServiceIndex.HostnameAndNamespace[host.Name(config.Name)][config.Namespace]
			if exist {
				if !push.IsServiceVisible(svc, proxy.Metadata.Namespace) {
					return false
				}
			}
		}
		return true
	default:
		// TODO We'll add the check for other proxy types later.
		return true
	}
	return false
}

// DefaultProxyNeedsPush check if a proxy needs push for this push event.
func DefaultProxyNeedsPush(proxy *model.Proxy, req *model.PushRequest) bool {
	if ConfigAffectsProxy(req, proxy) {
		return true
	}

	// If the proxy's service updated, need push for it.
	if len(proxy.ServiceInstances) > 0 && req.ConfigsUpdated != nil {
		svc := proxy.ServiceInstances[0].Service
		if _, ok := req.ConfigsUpdated[model.ConfigKey{
			Kind:      kind.ServiceEntry,
			Name:      string(svc.Hostname),
			Namespace: svc.Attributes.Namespace,
		}]; ok {
			return true
		}
	}

	return false
}
