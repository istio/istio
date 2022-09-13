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

// Package common contains resource names, which may vary from version to version.
package common

import (
	"fmt"

	"istio.io/pkg/log"
)

const (
	// latestKey is an arbitrary value that represents the fallback version (master).
	latestKey = "latest"

	ProxyContainerName     = "istio-proxy"
	DiscoveryContainerName = "discovery"
	OperatorContainerName  = "istio-operator"

	// namespaceAll is the default argument of across all namespaces
	NamespaceAll    = ""
	StrNamespaceAll = "allNamespaces"
)

type kv struct {
	k string
	v string
}

type resourceNames struct {
	discoveryLabels []kv
	istioDebugURLs  []string
	proxyDebugURLs  []string
}

var versionMap = map[string]*resourceNames{
	latestKey: {
		discoveryLabels: []kv{
			{k: "app", v: "istiod"},
		},
		istioDebugURLs: []string{
			"debug/adsz",
			"debug/syncz",
			"debug/registryz",
			"debug/endpointz",
			"debug/instancesz",
			"debug/endpointShardz",
			"debug/configz",
			"debug/cachez",
			"debug/resourcesz",
			"debug/authorizationz",
			"debug/push_status",
			"debug/inject",
			"debug/mesh",
			"debug/networkz",
		},
		proxyDebugURLs: []string{
			"certs",
			"clusters",
			"config_dump?include_eds",
			"listeners",
			"memory",
			"server_info",
			"stats/prometheus",
			"runtime",
		},
	},
}

// IstiodDebugURLs returns a list of Istiod debug URLs for the given version.
func IstiodDebugURLs(clusterVersion string) []string {
	return versionMap[getVersionKey(clusterVersion)].istioDebugURLs
}

// ProxyDebugURLs returns a list of proxy debug URLs for the given version.
func ProxyDebugURLs(clusterVersion string) []string {
	return versionMap[getVersionKey(clusterVersion)].proxyDebugURLs
}

// IsDiscoveryContainer reports whether the given container is an Istio discovery container for the given version.
// Labels are the labels for the given pod.
func IsDiscoveryContainer(clusterVersion, container string, labels map[string]string) bool {
	if container != DiscoveryContainerName {
		return false
	}

	for _, kv := range versionMap[getVersionKey(clusterVersion)].discoveryLabels {
		if labels[kv.k] != kv.v {
			return false
		}
	}
	return true
}

// IsProxyContainer reports whether container is an istio proxy container.
func IsProxyContainer(_, container string) bool {
	return container == ProxyContainerName
}

// IsOperatorContainer reports whether the container is an istio-operator container.
func IsOperatorContainer(_, container string) bool {
	return container == OperatorContainerName
}

func getVersionKey(clusterVersion string) string {
	if versionMap[clusterVersion] == nil {
		return latestKey
	}
	return clusterVersion
}

func LogAndPrintf(format string, a ...any) {
	fmt.Printf(format, a...)
	log.Info(fmt.Sprintf(format, a...))
}
