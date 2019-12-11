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

package authpolicy

import (
	"testing"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	authn "istio.io/api/authentication/v1alpha1"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestAuthPolicy_Input_Output(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, _, _ := setup(g, 0)

	g.Expect(xform.Inputs()).To(Equal(collection.Names{
		metadata.K8SAuthenticationIstioIoV1Alpha1Policies,
	}))
	g.Expect(xform.Outputs()).To(Equal(collection.Names{
		metadata.IstioAuthenticationV1Alpha1Policies,
	}))

	xform, _, _ = setup(g, 1)

	g.Expect(xform.Inputs()).To(Equal(collection.Names{
		metadata.K8SAuthenticationIstioIoV1Alpha1Meshpolicies,
	}))
	g.Expect(xform.Outputs()).To(Equal(collection.Names{
		metadata.IstioAuthenticationV1Alpha1Meshpolicies,
	}))
}

func TestAuthPolicy_AddSync(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		acc.Clear()
		xform.Start()

		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], input()))
		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))

		g.Eventually(acc.Events).Should(ConsistOf(
			event.AddFor(xform.Outputs()[0], output()),
			event.FullSyncFor(xform.Outputs()[0]),
		))
		xform.Stop()
	}
}

func TestAuthPolicy_SyncAdd(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)
		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], input()))

		g.Eventually(acc.Events).Should(ConsistOf(
			event.FullSyncFor(xform.Outputs()[0]),
			event.AddFor(xform.Outputs()[0], output()),
		))

		xform.Stop()
	}
}

func TestAuthPolicy_AddUpdateDelete(t *testing.T) {
	g := NewGomegaWithT(t)

	r2 := input()
	r2.Item.(*authn.Policy).OriginIsOptional = true

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], input()))
		src.Handlers.Handle(event.UpdateFor(xform.Inputs()[0], r2))
		src.Handlers.Handle(event.DeleteForResource(xform.Inputs()[0], r2))

		g.Eventually(acc.Events).Should(ConsistOf(
			event.FullSyncFor(xform.Outputs()[0]),
			event.AddFor(xform.Outputs()[0], output()),
			event.UpdateFor(xform.Outputs()[0], r2),
			event.DeleteForResource(xform.Outputs()[0], r2),
		))
		xform.Stop()
	}
}

func TestAuthPolicy_SyncReset(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.Event{Kind: event.Reset})

		g.Eventually(acc.Events).Should(ConsistOf(
			event.FullSyncFor(xform.Outputs()[0]),
			event.Event{Kind: event.Reset},
		))

		xform.Stop()
	}
}

func TestAuthPolicy_InvalidEventKind(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.Event{Kind: 55})

		g.Eventually(acc.Events).Should(ConsistOf(
			event.FullSyncFor(xform.Outputs()[0]),
		))

		xform.Stop()
	}
}

func TestAuthPolicy_NoListeners(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xforms := GetProviders().Create(processing.ProcessorOptions{})
		g.Expect(xforms).To(HaveLen(2))

		src := &fixtures.Source{}
		xform := xforms[i]
		src.Dispatch(xform)

		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.Event{Kind: event.Reset})
		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], input()))

		// No crash
		xform.Stop()
	}
}

func TestAuthPolicy_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		xform.Start()
		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], input()))

		g.Eventually(acc.Events).Should(ConsistOf(
			event.AddFor(xform.Outputs()[0], output()),
			event.FullSyncFor(xform.Outputs()[0]),
		))
		xform.Stop()
	}
}

func TestAuthPolicy_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], input()))

		g.Eventually(acc.Events).Should(ConsistOf(
			event.AddFor(xform.Outputs()[0], output()),
			event.FullSyncFor(xform.Outputs()[0]),
		))

		acc.Clear()

		xform.Stop()
		xform.Stop()

		g.Consistently(acc.Events).Should(BeEmpty())
	}
}

func TestAuthPolicy_StartStopStartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], input()))

		g.Eventually(acc.Events).Should(ConsistOf(
			event.AddFor(xform.Outputs()[0], output()),
			event.FullSyncFor(xform.Outputs()[0]),
		))

		acc.Clear()
		xform.Stop()
		g.Consistently(acc.Events).Should(BeEmpty())

		xform.Start()
		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))
		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], input()))

		g.Eventually(acc.Events).Should(ConsistOf(
			event.AddFor(xform.Outputs()[0], output()),
			event.FullSyncFor(xform.Outputs()[0]),
		))

		acc.Clear()
		xform.Stop()
		g.Consistently(acc.Events).Should(BeEmpty())
	}
}

func TestAuthPolicy_InvalidEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		xform.Start()

		src.Handlers.Handle(event.FullSyncFor(xform.Outputs()[0])) // Send output events
		src.Handlers.Handle(event.AddFor(xform.Outputs()[0], input()))

		g.Consistently(acc.Events).Should(BeEmpty())
		xform.Stop()
	}
}

func TestAuthPolicy_InvalidProto(t *testing.T) {
	g := NewGomegaWithT(t)

	r := input()
	r.Item = &types.Struct{}

	for i := 0; i < 2; i++ {
		xform, src, acc := setup(g, i)

		acc.Clear()
		xform.Start()

		src.Handlers.Handle(event.AddFor(xform.Inputs()[0], r))
		src.Handlers.Handle(event.FullSyncFor(xform.Inputs()[0]))

		g.Eventually(acc.Events).Should(ConsistOf( // No add event
			event.FullSyncFor(xform.Outputs()[0]),
		))
		xform.Stop()
	}
}

func setup(g *GomegaWithT, i int) (event.Transformer, *fixtures.Source, *fixtures.Accumulator) {
	xforms := GetProviders().Create(processing.ProcessorOptions{})
	g.Expect(xforms).To(HaveLen(2))

	src := &fixtures.Source{}
	acc := &fixtures.Accumulator{}
	src.Dispatch(xforms[i])
	xforms[i].DispatchFor(xforms[i].Outputs()[0], acc)

	return xforms[i], src, acc
}

func input() *resource.Entry {
	return &resource.Entry{
		Metadata: resource.Metadata{
			Name: resource.NewName("ns", "ap"),
		},
		Item: &authn.Policy{
			PeerIsOptional: true,
			Peers: []*authn.PeerAuthenticationMethod{
				{
					Params: &authn.PeerAuthenticationMethod_Mtls{
						Mtls: nil, // This is what the conversion is all about...
					},
				},
			},
		},
	}
}

func output() *resource.Entry {
	return &resource.Entry{
		Metadata: resource.Metadata{
			Name: resource.NewName("ns", "ap"),
		},
		Item: &authn.Policy{
			PeerIsOptional: true,
			Peers: []*authn.PeerAuthenticationMethod{
				{
					Params: &authn.PeerAuthenticationMethod_Mtls{
						Mtls: &authn.MutualTls{},
					},
				},
			},
		},
	}
}
