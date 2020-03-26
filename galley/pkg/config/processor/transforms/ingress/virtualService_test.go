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
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

func TestVirtualService_Input_Output(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, _, _ := setupVS(g, processing.ProcessorOptions{})

	g.Expect(xform.Inputs()).To(Equal(collection.NewSchemasBuilder().MustAdd(collections.K8SExtensionsV1Beta1Ingresses).Build()))
	g.Expect(xform.Outputs()).To(Equal(collection.NewSchemasBuilder().MustAdd(collections.IstioNetworkingV1Alpha3Virtualservices).Build()))
}

func TestVirtualService_AddSync(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices)))
}

func TestVirtualService_SyncAdd(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices),
		event.AddFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1()),
	))
}

func TestVirtualService_AddUpdateDelete(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.UpdateFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1v2()))
	src.Handlers.Handle(event.DeleteForResource(collections.K8SExtensionsV1Beta1Ingresses, ingress1v2()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices),
		event.AddFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.UpdateFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1v2()),
		event.DeleteFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1v2().Metadata.FullName, vs1v2().Metadata.Version),
	))
}

func TestVirtualService_SyncReset(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.Event{Kind: event.Reset})

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices),
		event.Event{Kind: event.Reset},
	))
}

func TestVirtualService_InvalidEventKind(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.Event{Kind: 55})

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices),
	))
}

func TestVirtualService_NoListeners(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
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

func TestVirtualService_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()
	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices),
	))
}

func TestVirtualService_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices),
	))

	acc.Clear()

	xform.Stop()
	xform.Stop()

	g.Consistently(acc.Events).Should(BeEmpty())
}

func TestVirtualService_StartStopStartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()

	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices),
	))

	acc.Clear()
	xform.Stop()
	g.Consistently(acc.Events).Should(BeEmpty())

	xform.Start()
	src.Handlers.Handle(event.FullSyncFor(collections.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(collections.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(collections.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices),
	))

	acc.Clear()
	xform.Stop()
	g.Consistently(acc.Events).Should(BeEmpty())
}

func TestVirtualService_InvalidEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	o := processing.ProcessorOptions{
		DomainSuffix: "cluster.local",
		MeshConfig:   meshConfig(),
	}

	xform, src, acc := setupVS(g, o)

	xform.Start()
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(collections.IstioNetworkingV1Alpha3Virtualservices))

	g.Consistently(acc.Events).Should(BeEmpty())
}

func setupVS(g *GomegaWithT, o processing.ProcessorOptions) (event.Transformer, *fixtures.Source, *fixtures.Accumulator) {
	xforms := GetProviders().Create(o)
	g.Expect(xforms).To(HaveLen(2))

	src := &fixtures.Source{}
	acc := &fixtures.Accumulator{}
	xform := xforms[1]
	src.Dispatch(xform)
	xform.DispatchFor(collections.IstioNetworkingV1Alpha3Virtualservices, acc)

	return xform, src, acc
}
