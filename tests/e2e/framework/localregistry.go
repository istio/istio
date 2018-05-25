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

// LocalRegistry provides in-cluster docker registry for test
type LocalRegistry struct {
	namespace  string
	istioctl   *Istioctl
	active     bool
	Kubeconfig string
	file       string
	hub        string
	tag        string
}

// NewLocalRegistry creates a new LocalRegistry
func NewLocalRegistry(namespace string, istioctl *Istioctl, file, kubeconfig, hub, tag string) *LocalRegistry {
	return &LocalRegistry{
		namespace:  namespace,
		istioctl:   istioctl,
		Kubeconfig: kubeconfig,
		file:       file,
		hub:        hub,
		tag:        tag,
	}
}

// Setup implements the Cleanable interface
// Deploy the local registry to the cluster
func (l *LocalRegistry) Setup() error {
	l.active = true
	return nil
}

// Teardown implements the Cleanable interface
func (l *LocalRegistry) Teardown() error {
	if err := util.KubeDelete(l.namespace, l.file, l.Kubeconfig); err != nil {
		log.Errorf("Kubectl delete %s failed", l.Kubeconfig)
		return err
	}
	l.active = false
	return nil
}
