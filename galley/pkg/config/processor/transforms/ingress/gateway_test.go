// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain ingressAdapter copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ingress

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
)

var (
	ingressAdapter = rt.DefaultProvider().GetAdapter(schema.MustGet().KubeCollections().MustFindByGroupVersionKind(resource.GroupVersionKind{
		Group:   "extensions",
		Version: "v1beta1",
		Kind:    "Ingress",
	}).Resource())
)

func TestGateway_Input_Output(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, _, _ := setupGW(g, processing.ProcessorOptions{})

	g.Expect(xform.Inputs()).To(Equal(collection.NewSchemasBuilder().MustAdd(collections.K8SExtensionsV1Beta1Ingresses).Build()))
	g.Expect(xform.Outputs()).To(Equal(collection.NewSchemasBuilder().MustAdd(collections.IstioNetworkingV1Alpha3Gateways).Build()))
}

func TestGateway_AddSync(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupGW(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Gateways, gw1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
	))
}

func TestGateway_SyncAdd(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform, src, acc := setupGW(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
		event.AddFor(collections.IstioNetworkingV1Alpha3Gateways, gw1()),
	))
}

func TestGateway_AddUpdateDelete(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform, src, acc := setupGW(g, o)

	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.UpdateFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1v2()))
	src.Handlers.Handle(event.DeleteForResource(collections.K8SExtensionsV1Beta1Ingresses, ingress1v2()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
		event.AddFor(collections.IstioNetworkingV1Alpha3Gateways, gw1()),
		event.UpdateFor(collections.IstioNetworkingV1Alpha3Gateways, gw1v2()),
		event.DeleteForResource(collections.IstioNetworkingV1Alpha3Gateways, gw1v2()),
	))
}

func TestGateway_SyncReset(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupGW(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.Event{Kind: event.Reset})

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
		event.Event{Kind: event.Reset},
	))
}

func TestGateway_InvalidEventKind(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupGW(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.Event{Kind: 55})

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
	))
}

func TestGateway_NoListeners(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshcfg.Default(),
	}

	xforms := GetProviders().Create(o)
	g.Expect(xforms).To(HaveLen(2))

	src := &fixtures.Source{}
	xform := xforms[0]
	src.Dispatch(xform)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.Event{Kind: event.Reset})
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	// No crash
}

func TestGateway_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupGW(g, o)

	xform.Start()
	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Gateways, gw1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
	))
}

func TestGateway_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupGW(g, o)

	xform.Start()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Gateways, gw1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
	))

	acc.Clear()

	xform.Stop()
	xform.Stop()

	g.Consistently(acc.Events).Should(BeEmpty())
}

func TestGateway_StartStopStartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupGW(g, o)

	xform.Start()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Gateways, gw1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
	))

	acc.Clear()
	xform.Stop()
	g.Consistently(acc.Events).Should(BeEmpty())

	xform.Start()
	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Gateways, gw1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Gateways),
	))

	acc.Clear()
	xform.Stop()
	g.Consistently(acc.Events).Should(BeEmpty())
}

func TestGateway_InvalidEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupGW(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices))

	g.Consistently(acc.Events).Should(BeEmpty())
}

func setupGW(g *GomegaWithT, o processing.ProcessorOptions) (event.Transformer, *fixtures.Source, *fixtures.Accumulator) {
	xforms := GetProviders().Create(o)
	g.Expect(xforms).To(HaveLen(2))

	src := &fixtures.Source{}
	acc := &fixtures.Accumulator{}
	xform := xforms[0]
	src.Dispatch(xform)
	xform.DispatchFor(collections.IstioNetworkingV1Alpha3Gateways, acc)

	xform.Start()

	return xform, src, acc
}
