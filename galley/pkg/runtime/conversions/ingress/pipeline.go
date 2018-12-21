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
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("ingress-pipeline", "Galley Ingress processing pipeline", 0)

// AddIngressPipeline attaches Ingress processing components to the given Pipeline builder.
func AddIngressPipeline(b *processing.PipelineBuilder) {
	// Collection for collecting gateways
	addGatewayPipeline(b)
	addVirtualServicePipeline(b)
}

func addGatewayPipeline(b *processing.PipelineBuilder) {

	// collection that will store enveloped gateway resources.
	c := processing.NewCollection()

	// Accumulator that will do direct transformation of the resource to a Gateway and store.
	a := processing.NewAccumulator(c, toEnvelopedGateway)

	// View for exposing the transformed gateway resources for snapshotting
	v := processing.NewCollectionView(metadata.Gateway.TypeURL, c, nil)

	// Register the accumulator for listening to ingress changes.
	b.AddHandler(metadata.IngressSpec.TypeURL, a)

	// Expose the gateway view for snapshotting.
	b.AddView(v)
}

func addVirtualServicePipeline(b *processing.PipelineBuilder) {
	//// Create a collection to store incoming ingresses
	//ingressCollection := processing.NewCollection()
	//ai := processing.NewAccumulator(ingressCollection, func(r resource.Entry)(interface{}, error) {
	//	return r.Item, nil
	//})
	//
	//// Create a view on top of the ingresses that generate the virtual services.
	//vsView := &virtualServiceView{
	//	ingresses: ingressCollection,
	//}
	//b.AddHandler(metadata.IngressSpec.TypeURL, ai)
	//b.AddView(vsView)
	//
	////processing.TransformFn()
	////acc := processing.NewAccumulator(c, func(e resource.Entry) (interface{}, error) {
	////	ingress, ok := e.Item.(*v1beta1.IngressSpec)
	////	if !ok {
	////		return nil, fmt.Errorf("invalid resource item encountered while expecting ingress: %v", e.Item)
	////	}
	////
	////	entry := conversions.IngressToGateway(e.ID, ingress)
	////})
	////
	////v := processing.NewCollectionView(metadata.Gateway.TypeURL, c)
	////
	////
	////acc := processing.NewEntryCollector()
	////b.AddHandler(metadata.IngressSpec.TypeURL, acc)
	////
	////gwView := newGatewayView(acc)
	////b.AddView(gwView)
	////
	////vsView := newVirtualServiceView(acc)
	////b.AddView(vsView)
}