//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package ingress

import (
	"reflect"
	"sort"
	"strings"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/conversions"
	"istio.io/istio/galley/pkg/runtime/conversions/envelope"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/resource"

	"k8s.io/api/extensions/v1beta1"
)

type virtualServiceView struct {
	generation int64
	collection *processing.EntryCollection
	config     *Config

	// track collections generation to detect any changes that should retrigger a rebuild of cached state
	lastCollectionGen     int64
	previousIngressByHost map[string]*resource.Entry
}

var _ processing.View = &virtualServiceView{}

func (v *virtualServiceView) rebuild() {
	if v.collection.Generation() == v.lastCollectionGen {
		// No need to rebuild
		return
	}

	// Order names for stable generation.
	var orderedNames []resource.FullName
	for _, name := range v.collection.Names() {
		orderedNames = append(orderedNames, name)
	}
	sort.Slice(orderedNames, func(i, j int) bool {
		return strings.Compare(orderedNames[i].String(), orderedNames[j].String()) < 0
	})

	ingressByHost := make(map[string]*resource.Entry)
	for _, name := range orderedNames {
		entry := v.collection.Item(name)
		ingress := entry.Item.(*v1beta1.IngressSpec) // TODO

		conversions.IngressToVirtualService(entry.ID, ingress, v.config.DomainSuffix, ingressByHost)
	}

	if v.previousIngressByHost == nil || !reflect.DeepEqual(v.previousIngressByHost, ingressByHost) {
		v.previousIngressByHost = ingressByHost
		v.lastCollectionGen = v.collection.Generation()
		v.generation++
	}
}

// Type implements processing.View
func (v *virtualServiceView) Type() resource.TypeURL {
	return metadata.VirtualService.TypeURL
}

// Generation implements processing.View
func (v *virtualServiceView) Generation() int64 {
	v.rebuild()
	return v.generation
}

// Get implements processing.View
func (v *virtualServiceView) Get() []*mcp.Envelope {
	v.rebuild()

	result := make([]*mcp.Envelope, 0, len(v.previousIngressByHost))
	for _, e := range v.previousIngressByHost {
		env, err := envelope.Envelope(*e)
		if err != nil {
			scope.Errorf("Unable to envelope virtual service resource: %v", err)
			continue
		}
		result = append(result, env)
	}

	return result
}
