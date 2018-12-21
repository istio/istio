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
	"istio.io/istio/galley/pkg/runtime/conversions"
	"istio.io/istio/galley/pkg/runtime/conversions/envelope"
	"istio.io/istio/galley/pkg/runtime/resource"
	"k8s.io/api/extensions/v1beta1"
)

func toEnvelopedGateway(entry resource.Entry) (interface{}, error) {
	ingress := entry.Item.(*v1beta1.IngressSpec) // TODO
	gwEntry := conversions.IngressToGateway(entry.ID, ingress)
	enveloped, err := envelope.Envelope(gwEntry)
	if err != nil {
		scope.Errorf("Unable to envelope and store resource %q: %v", gwEntry.ID.String(), err)
		return nil, err
	}

	return enveloped, err
}

//		gw := conversions.IngressToGateway(key, ingress)
//
//		gwState := s.entries[metadata.Gateway.TypeURL]
//		gwState.projections[gw.ID.FullName] = envelope
//		err = b.SetEntry(
//			metadata.Gateway.TypeURL.String(),
//			gw.ID.FullName.String(),
//			string(gw.ID.Version),
//			gw.ID.CreateTime,
//			gw.Item)


//
//import (
//	"istio.io/istio/galley/pkg/metadata"
//	"istio.io/istio/galley/pkg/runtime/conversions"
//	"istio.io/istio/galley/pkg/runtime/processing"
//	"istio.io/istio/galley/pkg/runtime/resource"
//	"istio.io/istio/pilot/pkg/model"
//	"k8s.io/api/extensions/v1beta1"
//)
//
//type gatewayHandler struct {
//	accumulator *processing.EnvelopeAccumulator
//}
//var _ processing.Handler = &gatewayHandler{}
//
//// Handle implements pipeline.Handler
//func (h *gatewayHandler) Handle(ev resource.Event) bool {
//	switch ev.Kind {
//	case resource.Added, resource.Updated:
//		ingress, ok := ev.Entry.Item.(*v1beta1.IngressSpec)
//		if !ok {
//			scope.Errorf("Error converting incoming item to ingress spec: %v", ev.Entry.Item)
//			return false
//		}
//
//		entry := conversions.IngressToGateway(ev.Entry.ID, ingress)
//		enveloped, err := processing.Envelope(entry)
//		if err != nil {
//			scope.Errorf("Unable to envelope and store resource %q: %v", ev.Entry.ID.String(), err)
//			return false
//		}
//		return h.accumulator.Set(entry.ID, enveloped)
//
//	case resource.Deleted:
//		key := generateGatewayKey(ev.Entry.ID)
//		return h.accumulator.Remove(key.FullName)
//
//	default:
//		scope.Errorf("Unknown event kind encountered when processing %q: %v", ev.Entry.ID.String(), ev.Kind)
//		return false
//	}
//}
//
//func generateGatewayKey(ikey resource.VersionedKey) resource.VersionedKey {
//	_, name := ikey.FullName.InterpretAsNamespaceAndName()
//
//	newName := name + "-" + model.IstioIngressGatewayName
//	newNamespace := model.IstioIngressNamespace
//
//	return resource.VersionedKey{
//		Version: ikey.Version,
//		CreateTime: ikey.CreateTime,
//		Key: resource.Key{
//			TypeURL: metadata.Gateway.TypeURL,
//			FullName: resource.FullNameFromNamespaceAndName(newNamespace, newName),
//		},
//	}
//}