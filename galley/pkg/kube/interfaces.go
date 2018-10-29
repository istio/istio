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

package kube

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // import GKE cluster authentication plugin
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Interfaces interface allows access to the Kubernetes API Service methods. It is mainly used for
// test/injection purposes.
type Interfaces interface {
	DynamicInterface(gv schema.GroupVersion, kind string, listKind string) (dynamic.Interface, error)
}

type kube struct {
	cfg *rest.Config
}

var _ Interfaces = &kube{}

// NewKubeFromConfigFile returns a new instance of Interfaces.
func NewKubeFromConfigFile(kubeconfig string) (Interfaces, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	return NewKube(config), nil
}

// NewKube returns a new instance of Interfaces.
func NewKube(cfg *rest.Config) Interfaces {
	return &kube{
		cfg: cfg,
	}
}

// DynamicInterface returns a new dynamic.Interface for the specified API Group/Version.
func (k *kube) DynamicInterface(gv schema.GroupVersion, kind, listKind string) (dynamic.Interface, error) {
	return dynamic.NewForConfig(k.cfg)
}
