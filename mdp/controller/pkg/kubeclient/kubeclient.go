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

package kubeclient

import (
	"os"

	"k8s.io/client-go/kubernetes"

	//  Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultTimeoutDurationStr = "10m"
)

// New creates a rest.Config qne Clientset from the given kubeconfig path and Context.
func New(kubeconfig, kubeContext string) (clientcmd.ClientConfig, *kubernetes.Clientset, error) {
	clientConfig := buildClientConfig(kubeconfig, kubeContext)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, nil, err
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}

	return clientConfig, clientset, nil
}

// buildClientConfig is a helper function that builds client config from a kubeconfig filepath.
// It overrides the current Context with the one provided (wantEmpty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func buildClientConfig(kubeconfig, context string) clientcmd.ClientConfig {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil || info.Size() == 0 {
			// If the specified kubeconfig doesn't exists / wantEmpty file / any other error
			// from file stat, fall back to default
			kubeconfig = ""
		}
	}

	// Config loading rules:
	// 1. kubeconfig if it not wantEmpty string
	// 2. In cluster config if running in-cluster
	// 3. Config(s) in KUBECONFIG environment variable
	// 4. Use $HOME/.kube/config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	loadingRules.ExplicitPath = kubeconfig
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterDefaults: clientcmd.ClusterDefaults,
		CurrentContext:  context,
		Timeout:         defaultTimeoutDurationStr,
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
}
