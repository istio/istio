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
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// BuildClientConfig builds a client rest config from a kubeconfig filepath and context.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientConfig(kubeconfig, context string) (*rest.Config, error) {
	return BuildClientCmd(kubeconfig, context).ClientConfig()
}

// BuildClientCmd builds a client cmd config from a kubeconfig filepath and context.
// It overrides the current context with the one provided (empty to use default).
//
// This is a modified version of k8s.io/client-go/tools/clientcmd/BuildConfigFromFlags with the
// difference that it loads default configs if not running in-cluster.
func BuildClientCmd(kubeconfig, context string) clientcmd.ClientConfig {
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil || info.Size() == 0 {
			// If the specified kubeconfig doesn't exists / empty file / any other error
			// from file stat, fall back to default
			kubeconfig = ""
		}
	}

	//Config loading rules:
	// 1. kubeconfig if it not empty string
	// 2. In cluster config if running in-cluster
	// 3. Config(s) in KUBECONFIG environment variable
	// 4. Use $HOME/.kube/config
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	loadingRules.ExplicitPath = kubeconfig
	configOverrides := &clientcmd.ConfigOverrides{
		ClusterDefaults: clientcmd.ClusterDefaults,
		CurrentContext:  context,
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
}

// CreateClientset is a helper function that builds a kubernetes Clienset from a kubeconfig
// filepath. See `BuildClientConfig` for kubeconfig loading rules.
func CreateClientset(kubeconfig, context string, fns ...func(*rest.Config)) (*kubernetes.Clientset, error) {
	c, err := BuildClientConfig(kubeconfig, context)
	if err != nil {
		return nil, err
	}
	for _, fn := range fns {
		fn(c)
	}
	return kubernetes.NewForConfig(c)
}

// CreateInterfaceFromClusterConfig is a helper function to create Kubernetes interface from in memory cluster config struct
func CreateInterfaceFromClusterConfig(clusterConfig *clientcmdapi.Config) (kubernetes.Interface, error) {
	return createInterface(clusterConfig)
}

// createInterface is new function which creates rest config and kubernetes interface
// from passed cluster's config struct
func createInterface(clusterConfig *clientcmdapi.Config) (kubernetes.Interface, error) {
	clientConfig := clientcmd.NewDefaultClientConfig(*clusterConfig, &clientcmd.ConfigOverrides{})
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}
