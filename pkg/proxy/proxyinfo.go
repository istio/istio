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

package proxy

import (
	"context"
	"encoding/json"
	"fmt"

	"istio.io/istio/pkg/kube"
	"istio.io/pkg/version"
)

// GetProxyInfo retrieves infos of proxies that connect to the Istio control plane of specific revision.
func GetProxyInfo(kubeconfig, configContext, revision, istioNamespace string) (*[]version.ProxyInfo, error) {
	kubeClient, err := kube.NewExtendedClient(kube.BuildClientCmd(kubeconfig, configContext), revision)
	if err != nil {
		return nil, err
	}
	// Ask Pilot for the Envoy sidecar sync status, which includes the sidecar version info
	allProxyInfo, err := kubeClient.AllDiscoveryDo(context.TODO(), istioNamespace, "/debug/proxy-info")
	if err != nil {
		return nil, err
	}

	pi := []version.ProxyInfo{}
	for _, info := range allProxyInfo {
		var proxyInfo []*version.ProxyInfo
		if err = json.Unmarshal(info, &proxyInfo); err != nil {
			return nil, err
		}
		for _, ss := range proxyInfo {
			pi = append(pi, *ss)
		}
	}
	return &pi, nil
}

// GetIDsFromProxyInfo is a helper function to retrieve list of IDs from Proxy.
func GetIDsFromProxyInfo(kubeconfig, configContext, revision, istioNamespace string) ([]string, error) {
	var IDs []string
	pi, err := GetProxyInfo(kubeconfig, configContext, revision, istioNamespace)
	if err != nil {
		return IDs, fmt.Errorf("failed to get proxy infos: %v", err)
	}
	for _, pi := range *pi {
		IDs = append(IDs, pi.ID)
	}
	return IDs, nil
}
