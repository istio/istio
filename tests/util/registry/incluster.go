// Copyright 2018 Istio Authors
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

package registry

import (
	"fmt"
	"path"
	"strings"

	"istio.io/istio/tests/util"
	"istio.io/istio/pkg/test/kube"

	"k8s.io/client-go/rest"
	"github.com/hashicorp/go-multierror"
)

const (
	// Default values for test env setup
	registryYAML       = "tests/util/registry/k8s_registry.yaml"
	registryNamespace  = "docker-registry"
	remoteRegistryPort = 5000
	kubeRegistry       = "kube-registry"
	kubeRegistryProxy  = "kube-registry-proxy"
	labelKey           = "k8s-app"
)

// NewInClusterRegistry creates a new in-cluster registry object.
func NewInClusterRegistry(kubeConfig string, localPort uint16) (*InClusterRegistry, error) {
	client, config, err := kube.NewRestClient(kubeConfig, "")
	if err != nil {
		return nil, err
	}
	accessor, err := kube.NewAccessor(config)
	if err != nil {
		return nil, err
	}
	return &InClusterRegistry{
		kubeconfig:        kubeConfig,
		client:            client,
		config:            config,
		accessor:          *accessor,
		localRegistryPort: localPort,
	}, nil
}

// InClusterRegistry helps deploying an in-cluster registry on a k8s cluster.
type InClusterRegistry struct {
	kubeconfig           string
	client               *rest.RESTClient
	config               *rest.Config
	accessor             kube.Accessor
	forwarder            kube.PortForwarder
	localRegistryAddress string
	localRegistryPort    uint16
}

// Start sets up registry and returns if any error was found while doing that.
func (r *InClusterRegistry) Start() error {

	if err := r.accessor.CreateNamespace(registryNamespace, ""); err != nil {
		if !strings.Contains(err.Error(), "already exist") {
			return err
		}
	}

	localRegistryFilePath := path.Join(util.GetResourcePath(registryYAML))
	if err := kube.Apply(r.kubeconfig, registryNamespace, localRegistryFilePath); err != nil {
		return err
	}

	if err := r.accessor.WaitUntilDeploymentIsReady(registryNamespace, kubeRegistry); err != nil {
		return err
	}

	if err := r.accessor.WaitUntilDaemonSetIsReady(registryNamespace, kubeRegistryProxy); err != nil {
		return err
	}

	registryPod, err := r.accessor.FindPodBySelectors(registryNamespace,
		fmt.Sprintf("%s=%s", labelKey, kubeRegistry))
	if err != nil {
		return err
	}

	// Registry is up now, try to get the registry pod for port-forwarding
	options := &kube.PodSelectOptions{
		PodNamespace: registryNamespace,
		PodName:      registryPod.Name,
	}
	ports := fmt.Sprintf("%d:%d", r.localRegistryPort, remoteRegistryPort)
	forwarder, err := kube.NewPortForwarder(r.client, r.config, options, ports)
	if err != nil {
		return err
	}

	if err := forwarder.ForwardPorts(); err != nil {
		return err
	}

	r.forwarder = forwarder
	r.localRegistryAddress = forwarder.Address()

	return nil
}

// GetLocalRegistryAddress returns the local registry address after a successful call to Start().
func (r *InClusterRegistry) GetLocalRegistryAddress() string {
	return r.localRegistryAddress
}

// Close deletes local registry from k8s cluster and cleans-up port forward processes too.
func (r *InClusterRegistry) Close() error {
	var errors error
	if err := kube.Delete(r.kubeconfig, registryNamespace, util.GetResourcePath(registryYAML)); err != nil {
		errors = multierror.Append(errors, err)
	}
	if err := r.accessor.DeleteNamespace(registryNamespace); err != nil {
		errors = multierror.Append(errors, err)
	}
	if r.forwarder != nil {
		r.forwarder.Close()
	}
	return errors
}
