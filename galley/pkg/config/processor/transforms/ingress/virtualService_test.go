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
	"bytes"
	"testing"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/gomega"
	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

var (
	virtualServiceAdapter = rt.DefaultProvider().GetAdapter(metadata.MustGet().KubeSource().Resources().MustFind(
		"networking.istio.io", "VirtualService"))
)

func TestVS_Input_Output(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, _, _ := setupVS(g)

	g.Expect(xform.Inputs()).To(Equal(collection.Names{metadata.K8SExtensionsV1Beta1Ingresses}))
	g.Expect(xform.Outputs()).To(Equal(collection.Names{metadata.IstioNetworkingV1Alpha3Virtualservices}))
}

func TestVS_AddSync(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)
	defer xform.Stop()

	src.Handlers.Handle(event.AddFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
	))
}

func TestVS_SyncAdd(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)
	defer xform.Stop()

	src.Handlers.Handle(event.AddFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
		event.AddFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1()),
	))
}

func TestVS_AddUpdateDelete(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1()))
	src.Handlers.Handle(event.UpdateFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1v2()))
	src.Handlers.Handle(event.DeleteForResource(metadata.K8SExtensionsV1Beta1Ingresses, ingress1v2()))

	//time.Sleep(time.Second)
	// res := []event.Event {
	// 	event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
	// 	event.AddFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1()),
	// 	event.UpdateFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1v2()),
	// 	event.DeleteForResource(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1v2()),
	// }
	// s := cmp.Diff(acc.Events(), res,
	// 	cmp.AllowUnexported(collection.Name{}),
	// 	cmp.AllowUnexported(resource.Name{}),
	// )
	// t.Logf(s)
	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
		event.AddFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.UpdateFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1v2()),
		event.DeleteForResource(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1v2()),
	))
}

func TestVS_SyncReset(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.Event{Kind: event.Reset})

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
		event.Event{Kind: event.Reset},
	))
}

func TestVS_InvalidEventKind(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.Event{Kind: 55})

	g.Eventually(acc.Events).Should(ConsistOf(
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
	))
}

func TestVS_NoListeners(t *testing.T) {
	g := NewGomegaWithT(t)

	xforms := Create()
	g.Expect(xforms).To(HaveLen(2))

	src := &fixtures.Source{}
	xform := xforms[0]
	src.Dispatch(xform)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshcfg.Default(),
	}
	xform.Start(o)
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.Event{Kind: event.Reset})
	src.Handlers.Handle(event.AddFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1()))

	// No crash
}

func TestVS_DoubleStart(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)
	xform.Start(o)
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
	))
}

func TestVS_DoubleStop(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)

	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
	))

	acc.Clear()

	xform.Stop()
	xform.Stop()

	g.Consistently(acc.Events).Should(BeEmpty())
}

func TestVS_StartStopStartStop(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)

	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
	))

	acc.Clear()
	xform.Stop()
	g.Consistently(acc.Events).Should(BeEmpty())

	xform.Start(o)
	src.Handlers.Handle(event.FullSyncFor(metadata.K8SExtensionsV1Beta1Ingresses))
	src.Handlers.Handle(event.AddFor(metadata.K8SExtensionsV1Beta1Ingresses, ingress1()))

	g.Eventually(acc.Events).Should(ConsistOf(
		event.AddFor(metadata.IstioNetworkingV1Alpha3Virtualservices, vs1()),
		event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices),
	))

	acc.Clear()
	xform.Stop()
	g.Consistently(acc.Events).Should(BeEmpty())
}

func TestVS_InvalidEvent(t *testing.T) {
	g := NewGomegaWithT(t)

	xform, src, acc := setupGW(g)

	o := processing.ProcessorOptions{
		DomainSuffix: "svc.local",
		MeshConfig:   meshConfig(),
	}
	xform.Start(o)
	defer xform.Stop()

	src.Handlers.Handle(event.FullSyncFor(metadata.IstioNetworkingV1Alpha3Virtualservices))

	g.Consistently(acc.Events).Should(BeEmpty())
}

func vs1v2() *resource.Entry {
	return &resource.Entry{
		Metadata: resource.Metadata{
			Name:        resource.NewName("", "istio-system/foo-bar-com-bar-istio-autogenerated-k8s-ingress"),
			Version:     "$ing_v1",
			Annotations: map[string]string{},
		},
		Item: parseVirtualService(`
{
        "gateways": [
          "istio-autogenerated-k8s-ingress"
        ],
        "hosts": [
          "foo.bar.com"
        ],
        "http": [
          {
            "match": [
              {
                "uri": {
                  "MatchType": {
                    "Exact": "/bar"
                  }
                }
              }
            ],
            "route": [
              {
                "destination": {
                  "host": "service2.{{.Namespace}}.svc.cluster.local",
                  "port": {
                    "Port": {
                      "Number": 2400
                    }
                  }
                },
                "weight": 100
              }
            ]
          },
          {
            "match": [
              {
                "uri": {
                  "MatchType": {
                    "Exact": "/foo"
                  }
                }
              }
            ],
            "route": [
              {
                "destination": {
                  "host": "service1.{{.Namespace}}.svc.cluster.local",
                  "port": {
                    "Port": {
                      "Number": 4200
                    }
                  }
                },
                "weight": 100
              }
            ]
          }
        ]
      }`),
	}
}

func vs1() *resource.Entry {
	return &resource.Entry{
		Metadata: resource.Metadata{
			Name:        resource.NewName("", "istio-system/foo-bar-com-bar-istio-autogenerated-k8s-ingress"),
			Version:     "$ing_v1",
			Annotations: map[string]string{},
		},
		Item: parseVirtualService(`
{
        "gateways": [
          "istio-autogenerated-k8s-ingress"
        ],
        "hosts": [
          "foo.bar.com"
        ],
        "http": [
          {
            "match": [
              {
                "uri": {
                  "MatchType": {
                    "Exact": "/bar"
                  }
                }
              }
            ],
            "route": [
              {
                "destination": {
                  "host": "service2.{{.Namespace}}.svc.cluster.local",
                  "port": {
                    "Port": {
                      "Number": 2400
                    }
                  }
                },
                "weight": 100
              }
            ]
          },
          {
            "match": [
              {
                "uri": {
                  "MatchType": {
                    "Exact": "/foo"
                  }
                }
              }
            ],
            "route": [
              {
                "destination": {
                  "host": "service1.{{.Namespace}}.svc.cluster.local",
                  "port": {
                    "Port": {
                      "Number": 4200
                    }
                  }
                },
                "weight": 100
              }
            ]
          }
        ]
      }`),
	}
}

func parseVirtualService(s string) proto.Message {
	p := &v1alpha3.VirtualService{}
	b := bytes.NewReader([]byte(s))
	err := jsonpb.Unmarshal(b, p)
	if err != nil {
		panic(err)
	}
	return p
}

func setupVS(g *GomegaWithT) (event.Transformer, *fixtures.Source, *fixtures.Accumulator) {
	xforms := Create()
	g.Expect(xforms).To(HaveLen(2))

	src := &fixtures.Source{}
	acc := &fixtures.Accumulator{}
	xform := xforms[1]
	src.Dispatch(xform)
	xform.Select(metadata.IstioNetworkingV1Alpha3Virtualservices, acc)

	return xform, src, acc
}
