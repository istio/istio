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
	"strings"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1beta1"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/util/protomarshal"
)

type EffectiveProxyConfigResolver struct {
	meshConfig    *meshconfig.MeshConfig
	rootNamespace string
	root          *v1beta1.ProxyConfig
	namespace     map[string]*v1beta1.ProxyConfig
	workload      map[string]*v1beta1.ProxyConfig
}

// ImageType returns the effective image type for the given pod.
func (e *EffectiveProxyConfigResolver) ImageType(pod *resource.Instance) string {
	variant := ""
	if e.meshConfig.GetDefaultConfig().GetImage().GetImageType() != "" {
		variant = e.meshConfig.GetDefaultConfig().GetImage().GetImageType()
	}
	if e.root.GetImage().GetImageType() != "" {
		variant = e.root.GetImage().GetImageType()
	}
	if v, ok := e.namespace[pod.Metadata.FullName.Namespace.String()]; ok {
		if v.GetImage().GetImageType() != "" {
			variant = v.GetImage().GetImageType()
		}
	}
	// check if there are workload level resources that match the pod
	for k, v := range e.workload {
		if !strings.HasPrefix(k, pod.Metadata.FullName.Namespace.String()) {
			continue
		}
		if maps.Contains(pod.Metadata.Labels, v.GetSelector().GetMatchLabels()) {
			if v.GetImage().GetImageType() != "" {
				variant = v.GetImage().GetImageType()
			}
		}
	}
	if v, ok := pod.Metadata.Annotations[annotation.ProxyConfig.Name]; ok {
		pc := &meshconfig.ProxyConfig{}
		if err := protomarshal.ApplyYAML(v, pc); err == nil {
			if pc.GetImage().GetImageType() != "" {
				variant = pc.GetImage().GetImageType()
			}
		}
	}
	if variant == "default" {
		variant = ""
	}
	return variant
}

func NewEffectiveProxyConfigResolver(c analysis.Context) *EffectiveProxyConfigResolver {
	mc := &meshconfig.MeshConfig{}
	rootNamespace := ""
	c.ForEach(gvk.MeshConfig, func(r *resource.Instance) bool {
		meshConfig := r.Message.(*meshconfig.MeshConfig)
		rootNamespace = meshConfig.GetRootNamespace()
		if rootNamespace == "" {
			rootNamespace = "istio-system"
		}
		mc = meshConfig
		return true
	})

	resolver := &EffectiveProxyConfigResolver{
		meshConfig:    mc,
		rootNamespace: rootNamespace,
		namespace:     make(map[string]*v1beta1.ProxyConfig),
		workload:      make(map[string]*v1beta1.ProxyConfig),
	}

	c.ForEach(gvk.ProxyConfig, func(r *resource.Instance) bool {
		proxyConfig := r.Message.(*v1beta1.ProxyConfig)
		if r.Metadata.FullName.Namespace.String() == resolver.rootNamespace {
			resolver.root = proxyConfig
			return true
		}
		if proxyConfig.GetSelector() == nil {
			resolver.namespace[r.Metadata.FullName.Namespace.String()] = proxyConfig
		} else {
			resolver.workload[r.Metadata.FullName.String()] = proxyConfig
		}
		return true
	})
	return resolver
}
