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

// Package kube implements the shared and reusable library for Kubernetes
package kube

import (
	"fmt"
	"os"
	"os/user"

	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/log"
	// import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

// ResolveConfig checks whether to use the in-cluster or out-of-cluster config
func ResolveConfig(kubeconfig string) (string, error) {
	// Consistency with kubectl
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	if kubeconfig == "" {
		usr, err := user.Current()
		if err == nil {
			defaultCfg := usr.HomeDir + "/.kube/config"
			_, err := os.Stat(kubeconfig)
			if err != nil {
				kubeconfig = defaultCfg
			}
		}
	}
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("kubernetes configuration file %q does not exist", kubeconfig)
			} else {
				err = multierror.Append(err, fmt.Errorf("kubernetes configuration file %q", kubeconfig))
			}
			return "", err
		}

		// if it's an empty file, switch to in-cluster config
		if info.Size() == 0 {
			log.Info("using in-cluster configuration")
			return "", nil
		}
	}
	return kubeconfig, nil
}

// CreateInterface is a helper function to create Kubernetes interface from kubeconfig file
func CreateInterface(kubeconfig string) (kubernetes.Interface, error) {
	if len(kubeconfig) == 0 {
		// Avoid the confusing "Things might not work" message
		restConfig, err := restclient.InClusterConfig()
		if err != nil {
			return nil, err
		}
		return kubernetes.NewForConfig(restConfig)
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}

// CreateInterfaceFromClusterConfig is a helper function to create Kubernetes interface from in memory cluster config struct
func CreateInterfaceFromClusterConfig(clusterConfig *clientcmdapi.Config) (kubernetes.Interface, error) {
	return createInterface(clusterConfig)
}

// createInterface is new function which creates rest config and kubernetes interface
// from passed cluster's config struct
func createInterface(clusterConfig *clientcmdapi.Config) (kubernetes.Interface, error) {
	clientConfig := clientcmd.NewDefaultClientConfig(*clusterConfig, &clientcmd.ConfigOverrides{})
	rest, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(rest)
}
