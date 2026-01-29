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

package util

import (
	"fmt"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
)

// KubeconfigForCluster returns a minimal kubeconfig YAML for the provided cluster.
func KubeconfigForCluster(c cluster.Cluster) ([]byte, error) {
	filenamer, ok := c.(istioctl.Filenamer)
	if !ok {
		return nil, fmt.Errorf("cluster does not support fetching kubeconfig")
	}
	cfg, err := clientcmd.LoadFromFile(filenamer.Filename())
	if err != nil {
		return nil, err
	}
	contextName := cfg.CurrentContext
	if contextName == "" {
		for name := range cfg.Contexts {
			contextName = name
			break
		}
	}
	ctx := cfg.Contexts[contextName]
	if ctx == nil {
		return nil, fmt.Errorf("kubeconfig missing context %q", contextName)
	}

	clusterName := ctx.Cluster
	authName := ctx.AuthInfo
	clusterCfg := cfg.Clusters[clusterName]
	if clusterCfg == nil {
		return nil, fmt.Errorf("kubeconfig missing cluster %q", clusterName)
	}
	authCfg := cfg.AuthInfos[authName]
	if authCfg == nil {
		return nil, fmt.Errorf("kubeconfig missing auth info %q", authName)
	}

	clusterCopy := *clusterCfg
	authCopy := *authCfg
	name := c.Name()
	newCfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			name: &clusterCopy,
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			name: &authCopy,
		},
		Contexts: map[string]*clientcmdapi.Context{
			name: {
				Cluster:  name,
				AuthInfo: name,
			},
		},
		CurrentContext: name,
	}

	return clientcmd.Write(newCfg)
}
