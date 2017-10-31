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

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	// import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

// ResolveConfig checks whether to use the in-cluster or out-of-cluster config
func ResolveConfig(kubeconfig string) (string, error) {
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
			glog.Info("using in-cluster configuration")
			return "", nil
		}
	}
	return kubeconfig, nil
}

// CreateInterface is a helper function to create Kubernetes interface
func CreateInterface(kubeconfig string) (*rest.Config, kubernetes.Interface, error) {
	kube, err := ResolveConfig(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	config, err := clientcmd.BuildConfigFromFlags("", kube)
	if err != nil {
		return nil, nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	return config, client, err
}
