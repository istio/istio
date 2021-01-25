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

package util

import (
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/resource"
)

// Ref: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/#viewing-namespaces
// "kube-system": The namespace for objects created by the Kubernetes system.
// "kube-public": This namespace is mostly reserved for cluster usage.
// "kube-node-lease": This namespace for the lease objects associated with each node
//    which improves the performance of the node heartbeats as the cluster scales.
// "local-path-storage": Dynamically provisioning persistent local storage with Kubernetes.
//    used with Kind cluster: https://github.com/rancher/local-path-provisioner
var (
	SystemNamespaces = []string{
		constants.KubeSystemNamespace,
		constants.KubePublicNamespace,
		constants.KubeNodeLeaseNamespace,
		constants.LocalPathStorageNamespace,
	}
)

// IsSystemNamespace returns true for system namespaces
func IsSystemNamespace(ns resource.Namespace) bool {
	return IsIncluded(SystemNamespaces, ns.String())
}

// IsIstioControlPlane returns true for resources that are part of the Istio control plane
func IsIstioControlPlane(r *resource.Instance) bool {
	if _, ok := r.Metadata.Labels["istio"]; ok {
		return true
	}
	if r.Metadata.Labels["release"] == "istio" {
		return true
	}
	return false
}

// IsIncluded check if the term exists in a slice of string
func IsIncluded(slice []string, term string) bool {
	for _, val := range slice {
		if val == term {
			return true
		}
	}
	return false
}
