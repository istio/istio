// Copyright 2017 Istio Authors
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

package kube

import (
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// BuildClientConfig is a helper function that builds client config from a kubeconfig filepath.
// If kubeconfig is empty:
//   1. first try to get in-cluster config, if failed:
//   2. get default config
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that the kubeconfig can also contain multiple paths representing precedences
// of kubernetes config paths.
func BuildClientConfig(kubeconfig string) (*rest.Config, error) {
	var loadingRules *clientcmd.ClientConfigLoadingRules
	if kubeconfig == "" {
		// Try using the inClusterConfig
		config, err := rest.InClusterConfig()
		if err == nil {
			// Using In-Cluster client config
			return config, nil
		}

		//Using default config loading rules are:
		// 1. get all files from the $KUBECONFIG environment variable and chain them
		// 2. if the env is not set, use $HOME/.kube/config
		loadingRules = clientcmd.NewDefaultClientConfigLoadingRules()
	} else {
		// The kubeconfig may have multiple paths, add them all
		configs := filepath.SplitList(kubeconfig)
		loadingRules = &clientcmd.ClientConfigLoadingRules{Precedence: configs}
	}
	configOverrides := &clientcmd.ConfigOverrides{}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}
