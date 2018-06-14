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

package framework

import (
	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

const (
	// Default values for local test env setup
	LocalRegistryFile      = "tests/e2e/local/localregistry/localregistry.yaml"
	LocalRegistryNamespace = "kube-system"
)

// LocalRegistry provides in-cluster docker registry for test
type LocalRegistry struct {
	namespace  string
	istioctl   *Istioctl
	active     bool
	Kubeconfig string
}

// GetLocalRegistry detects and returns LocalRegistry if existed
func GetLocalRegistry(istioctl *Istioctl, kubeconfig string) *LocalRegistry {
	if _, err := util.Shell("kubectl get service kube-registry -n %s", LocalRegistryNamespace); err != nil {
		return nil
	}
	return &LocalRegistry{
		istioctl:   istioctl,
		Kubeconfig: kubeconfig,
	}
}

// Setup implements the Cleanable interface
func (l *LocalRegistry) Setup() error {
	l.active = true
	return nil
}

// Teardown implements the Cleanable interface
func (l *LocalRegistry) Teardown() error {
	if err := util.KubeDelete(LocalRegistryNamespace, LocalRegistryFile, l.Kubeconfig); err != nil {
		log.Errorf("Kubectl delete %s failed", l.Kubeconfig)
		return err
	}
	l.active = false
	return nil
}
