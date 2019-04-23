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
	"io/ioutil"
	"os"
	"strings"
	"text/template"

	"istio.io/istio/pkg/test/kube"

	multierror "github.com/hashicorp/go-multierror"
)

const (
	// Default values for test env setup
	kubeRegistry      = "kube-registry"
	kubeRegistryProxy = "kube-registry-proxy"
	labelKey          = "k8s-app"
)

// NewInClusterRegistry creates a new in-cluster registry object.
func NewInClusterRegistry(accessor *kube.Accessor, localPort uint16, ns string) (*InClusterRegistry, error) {
	return &InClusterRegistry{
		accessor:          accessor,
		localRegistryPort: localPort,
		namespace:         ns,
	}, nil
}

// InClusterRegistry helps deploying an in-cluster registry on a k8s cluster.
type InClusterRegistry struct {
	accessor             *kube.Accessor
	forwarder            kube.PortForwarder
	localRegistryAddress string
	localRegistryPort    uint16
	namespace            string
}

func writeTemplate(tmpl *template.Template, data interface{}, dst string) error {
	f, err := os.Create(dst)
	defer func() { _ = f.Close() }()
	if err != nil {
		return err
	}
	if err := tmpl.Execute(f, data); err != nil {
		return err
	}
	return nil
}

func writeTemplateInTempfile(tmpl *template.Template, data interface{}) (string, error) {
	dst, err := ioutil.TempFile("", "in_cluster_registry")
	if err != nil {
		return "", err
	}
	return dst.Name(), writeTemplate(tmpl, data, dst.Name())
}

// Start sets up registry and returns if any error was found while doing that.
func (r *InClusterRegistry) Start() error {

	if err := r.accessor.CreateNamespace(r.namespace, "", false); err != nil {
		if !strings.Contains(err.Error(), "already exist") {
			return err
		}
	}
	registryTemplate, err := getRegistryYAMLTemplate()
	if err != nil {
		return err
	}
	registryFile, err := writeTemplateInTempfile(
		registryTemplate,
		registryOptions{Namespace: r.namespace, Port: r.localRegistryPort})
	if err != nil {
		return err
	}
	if err := r.accessor.Apply(r.namespace, registryFile); err != nil {
		return err
	}

	if err := r.accessor.WaitUntilDeploymentIsReady(r.namespace, kubeRegistry); err != nil {
		return err
	}

	if err := r.accessor.WaitUntilDaemonSetIsReady(r.namespace, kubeRegistryProxy); err != nil {
		return err
	}

	registryPod, err := r.accessor.FindPodBySelectors(r.namespace,
		fmt.Sprintf("%s=%s", labelKey, kubeRegistry))
	if err != nil {
		return err
	}

	// Registry is up now, try to get the registry pod for port-forwarding
	forwarder, err := r.accessor.NewPortForwarder(registryPod, r.localRegistryPort, r.localRegistryPort)
	if err != nil {
		return err
	}

	if err := forwarder.Start(); err != nil {
		return err
	}

	r.forwarder = forwarder
	r.localRegistryAddress = forwarder.Address()

	return nil
}

// Address returns the local registry address after a successful call to Start().
func (r *InClusterRegistry) Address() string {
	return r.localRegistryAddress
}

// Close deletes local registry from k8s cluster and cleans-up port forward processes too.
func (r *InClusterRegistry) Close() error {
	var errors error
	if err := r.accessor.DeleteNamespace(r.namespace); err != nil {
		errors = multierror.Append(errors, err)
	}
	if err := r.accessor.WaitForNamespaceDeletion(r.namespace); err != nil {
		errors = multierror.Append(errors, err)
	}
	if r.forwarder != nil {
		r.forwarder.Close()
	}
	return errors
}
