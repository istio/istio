// Copyright 2019 Istio Authors
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

// Embed the core metadata file containing the collections as a resource
//go:generate go-bindata --nocompress --nometadata --pkg builtin -o builtin.gen.go testdata/

package builtin

import (
	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/testing/data"
)

var (
	// EndpointsCollection for testing
	EndpointsCollection = collection.NewName("k8s/endpoints")

	// NodesCollection for testing
	NodesCollection = collection.NewName("k8s/nodes")

	// PodsCollection for testing
	PodsCollection = collection.NewName("k8s/pods")

	// ServicesCollection for testing
	ServicesCollection = collection.NewName("k8s/services")
)

// GetEndpoints returns Endpoints test data
func GetEndpoints() string {
	return string(data.MustAsset("builtin/endpoints.yaml"))
}

// GetNode returns Node test data
func GetNode() string {
	return string(data.MustAsset("builtin/node.yaml"))
}

// GetPod returns Pod test data
func GetPod() string {
	return string(data.MustAsset("builtin/pod.yaml"))
}

// GetService returns Service test data
func GetService() string {
	return string(data.MustAsset("builtin/service.yaml"))
}

// GetNamespace returns Namespace test data
func GetNamespace() string {
	return string(data.MustAsset("builtin/namespace.yaml"))
}

// GetIngress returns Ingress test data
func GetIngress() string {
	return string(data.MustAsset("builtin/ingress.yaml"))
}
