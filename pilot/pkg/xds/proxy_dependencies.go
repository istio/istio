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
	"strings"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/credentials"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// UnAffectedConfigKinds contains config types which does not affect certain proxy types.
var UnAffectedConfigKinds = map[model.NodeType]sets.Set[kind.Kind]{
	// For Gateways, we do not care about the following configs for example Sidecar.
	model.Router: sets.New(kind.Sidecar),
	// For Sidecar, we do not care about the following configs for example Gateway.
	model.SidecarProxy: sets.New(kind.Gateway),
}

// filterRelevantUpdates filters PushRequest.ConfigsUpdated so that only configs relevant to the proxy are included,
// returning the original PushRequest if no filtering was needed or a new PushRequest if some config filtering was performed.
// The returned PushRequest should not be modified since it might be the original global PushRequest.
func filterRelevantUpdates(proxy *model.Proxy, req *model.PushRequest) *model.PushRequest {
	if len(req.ConfigsUpdated) == 0 {
		return req
	}

	relevantUpdates := make(sets.Set[model.ConfigKey])
	changed := false
	for config := range req.ConfigsUpdated {
		if proxyDependentOnConfig(proxy, config, req.Push) {
			relevantUpdates.Insert(config)
		} else {
			// we have filtered out a config
			changed = true
		}
	}

	// If the proxy's service updated, need push for it.
	if len(proxy.ServiceTargets) > 0 && req.ConfigsUpdated != nil {
		for _, svc := range proxy.ServiceTargets {
			key := model.ConfigKey{
				Kind:      kind.ServiceEntry,
				Name:      string(svc.Service.Hostname),
				Namespace: svc.Service.Attributes.Namespace,
			}
			if req.ConfigsUpdated.Contains(key) {
				relevantUpdates.Insert(key)
			}
		}
	}

	if changed {
		newPushRequest := *req
		newPushRequest.ConfigsUpdated = relevantUpdates
		return &newPushRequest
	}

	return req
}

func proxyDependentOnConfig(proxy *model.Proxy, config model.ConfigKey, push *model.PushContext) bool {
	// Skip config dependency check based on proxy type for certain configs.
	if UnAffectedConfigKinds[proxy.Type].Contains(config.Kind) {
		return false
	}
	// Ambient Address updates only matter to proxies subscribed to Workload Address resources;
	// anything sidecars and gateways need from those changes is surfaced as ServiceEntry or
	// Endpoints updates.
	if features.ScopedAddressPushes && config.Kind == kind.Address {
		return proxy.GetWatchedResource(v3.AddressType) != nil
	}
	// Secret/ConfigMap updates only matter to proxies that subscribe to the specific resource via SDS.
	if config.Kind == kind.Secret {
		// Proxies using WasmPlugin/TrafficExtension might use image pull credentials outside of SDS subscriptions,
		// so for those proxies, we need to preserve the old behavior of pushing at every change.
		// TODO: build a reverse index (secret -> TrafficExtension resource names) in PushContext so we
		// can precisely match instead of pushing on each Secret change
		if proxy.GetWatchedResource(v3.ExtensionConfigurationType) != nil {
			return true
		}
		return proxyReferencesSecret(proxy, config)
	}
	if config.Kind == kind.ConfigMap {
		return proxyReferencesConfigMap(proxy, config)
	}
	// Detailed config dependencies check.
	switch proxy.Type {
	case model.SidecarProxy:
		if proxy.SidecarScope.DependsOnConfig(config, push.Mesh.RootNamespace) {
			return true
		} else if proxy.PrevSidecarScope != nil && proxy.PrevSidecarScope.DependsOnConfig(config, push.Mesh.RootNamespace) {
			return true
		}
	case model.Router:
		if config.Kind == kind.ServiceEntry {
			// If config is ServiceEntry, name of the config is service's FQDN
			if features.FilterGatewayClusterConfig && !push.ServiceAttachedToGateway(config.Name, config.Namespace, proxy) {
				return false
			}

			hostname := host.Name(config.Name)
			// gateways have default sidecar scopes
			if proxy.SidecarScope.GetService(hostname) == nil &&
				proxy.PrevSidecarScope.GetService(hostname) == nil {
				// skip the push when the service is not visible to the gateway,
				// and the old service is not visible/existent
				return false
			}
		}
		return true
	default:
		// TODO We'll add the check for other proxy types later.
		return true
	}
	return false
}

// proxyReferencesSecret checks whether the proxy's SDS subscriptions include the given secret.
func proxyReferencesSecret(proxy *model.Proxy, config model.ConfigKey) bool {
	sds := proxy.GetWatchedResource(v3.SecretType)
	if sds == nil || sds.ResourceNames.Len() == 0 {
		return false
	}
	secretName := config.Name
	secretNS := config.Namespace
	proxyNS := proxy.GetNamespace()

	// Build candidate names: the secret itself and its -cacert counterpart.
	names := []string{secretName}
	if stripped := strings.TrimSuffix(secretName, credentials.SdsCaSuffix); stripped != secretName {
		names = append(names, stripped)
	} else {
		names = append(names, secretName+credentials.SdsCaSuffix)
	}

	for _, name := range names {
		if proxyNS == secretNS && sds.ResourceNames.Contains(credentials.KubernetesSecretTypeURI+name) {
			return true
		}
		if sds.ResourceNames.Contains(credentials.KubernetesSecretTypeURI + secretNS + "/" + name) {
			return true
		}
		if sds.ResourceNames.Contains(credentials.KubernetesGatewaySecretTypeURI + secretNS + "/" + name) {
			return true
		}
	}
	return false
}

// proxyReferencesConfigMap checks whether the proxy's SDS subscriptions include the given configmap.
func proxyReferencesConfigMap(proxy *model.Proxy, config model.ConfigKey) bool {
	sds := proxy.GetWatchedResource(v3.SecretType)
	if sds == nil {
		return false
	}
	return sds.ResourceNames.Contains(credentials.KubernetesConfigMapTypeURI + config.Namespace + "/" + config.Name)
}

// DefaultProxyNeedsPush check if a proxy needs push for this push event and returns a new PushRequest in the case
// it needs to filter relevant updates for this proxy.
func DefaultProxyNeedsPush(proxy *model.Proxy, req *model.PushRequest) (*model.PushRequest, bool) {
	if req.Forced {
		return req, true
	}

	if proxy.IsWaypointProxy() || proxy.IsZTunnel() || proxy.IsAgentgateway() {
		// Optimizations do not apply since scoping uses different mechanism
		// TODO: implement ambient aware scoping
		return req, true
	}

	req = filterRelevantUpdates(proxy, req)
	return req, len(req.ConfigsUpdated) > 0
}
