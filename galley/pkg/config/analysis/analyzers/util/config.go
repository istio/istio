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

package util

import (
	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
)

// MeshConfig returns the mesh configuration object associated with the context
// Analyzers that call this should include metadata.IstioMeshV1Alpha1MeshConfig as an input in their Metadata
func MeshConfig(ctx analysis.Context) *v1alpha1.MeshConfig {
	// Only one MeshConfig should exist in practice, but getting it this way avoids needing
	// to plumb through the name or enforce/expose a constant.
	var mc *v1alpha1.MeshConfig
	ctx.ForEach(collections.IstioMeshV1Alpha1MeshConfig.Name(), func(r *resource.Instance) bool {
		mc = r.Message.(*v1alpha1.MeshConfig)
		return true
	})

	return mc
}

// IsSystemNamespace returns true for system namespaces
func IsSystemNamespace(ns resource.Namespace) bool {
	return ns == "kube-system" || ns == "kube-public"
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
