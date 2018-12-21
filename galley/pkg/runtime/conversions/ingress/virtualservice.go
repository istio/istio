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
//
//import (
//	"sort"
//	"strings"
//
//	mcp "istio.io/api/mcp/v1alpha1"
//	"istio.io/istio/galley/pkg/metadata"
//	"istio.io/istio/galley/pkg/runtime/conversions"
//	"istio.io/istio/galley/pkg/runtime/processing"
//	"istio.io/istio/galley/pkg/runtime/resource"
//	"k8s.io/api/extensions/v1beta1"
//)
//
//type virtualServiceView struct {
//	generation int64
//	ingresses *processing.Collection
//}
//
//var _ processing.View = &virtualServiceView{}
//
//func (v *virtualServiceView) Type() resource.TypeURL {
//	return metadata.VirtualService.TypeURL
//}
//
//func (v *virtualServiceView) Generation() int64 {
//	return v.ingresses.Generation()
//}
//
//func (v *virtualServiceView) Get() []*mcp.Envelope {
//	// Order names for stable generation.
//	var orderedNames []resource.FullName
//	for _, name := range v.ingresses.Names() {
//		orderedNames = append(orderedNames, name)
//	}
//	sort.Slice(orderedNames, func(i, j int) bool {
//		return strings.Compare(orderedNames[i].String(), orderedNames[j].String()) < 0
//	})
//
//
//		for _, name := range orderedNames {
//			ingress := v.ingresses.Item(name).(*v1beta1.IngressSpec)
//			version := v.ingresses.Version(name)
//			key := resource.Key{
//				TypeURL: metadata.IngressSpec.TypeURL,
//				FullName: name,
//			}
//			vkey := resource.VersionedKey{
//				Version: version,
//				Key: vkey,
//			}
//	//
//	//		ingress, err := conversions.ToIngressSpec(entry)
//	//		key := extractKey(name, entry, state.versions[name])
//	//		if err != nil {
//	//			// Shouldn't happen
//	//			scope.Errorf("error during ingress projection: %v", err)
//	//			continue
//	//		}
//			conversions.IngressToVirtualService(key, ingress, s.config.DomainSuffix, ingressByHost)
//	//
//	//		gw := conversions.IngressToGateway(key, ingress)
//	//
//	//		gwState := s.entries[metadata.Gateway.TypeURL]
//	//		gwState.projections[gw.ID.FullName] = envelope
//	//		err = b.SetEntry(
//	//			metadata.Gateway.TypeURL.String(),
//	//			gw.ID.FullName.String(),
//	//			string(gw.ID.Version),
//	//			gw.ID.CreateTime,
//	//			gw.Item)
//	//		if err != nil {
//	//			scope.Errorf("Unable to set gateway entry: %v", err)
//	//		}
//	//
//	//		// TODO: This is borked
//	//		b.SetVersion(metadata.Gateway.TypeURL.String(), string(gw.ID.Version))
//		}
//
//	//	for _, e := range ingressByHost {
//	//		err := b.SetEntry(
//	//			metadata.VirtualService.TypeURL.String(),
//	//			e.ID.FullName.String(),
//	//			string(e.ID.Version),
//	//			e.ID.CreateTime,
//	//			e.Item)
//	//		if err != nil {
//	//			scope.Errorf("Unable to set virtualservice entry: %v", err)
//	//		}
//	//		// TODO: This is borked
//	//		b.SetVersion(metadata.VirtualService.TypeURL.String(), string(e.ID.Version))
//	//	}
//}
