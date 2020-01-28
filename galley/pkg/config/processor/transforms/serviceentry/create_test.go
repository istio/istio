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

package serviceentry_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	"istio.io/api/annotation"
	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processing/snapshotter/strategy"
	"istio.io/istio/galley/pkg/config/processor/transforms/serviceentry"
	"istio.io/istio/galley/pkg/config/processor/transforms/serviceentry/pod"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/mcp/snapshot"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	domain    = "company.com"
	clusterIP = "10.0.0.10"
	pod1IP    = "10.0.0.1"
	pod2IP    = "10.0.0.2"
	namespace = resource.Namespace("fakeNamespace")
	nodeName  = "node1"
	region    = "region1"
	zone      = "zone1"
)

var (
	serviceName = resource.NewFullName(namespace, "svc1")
	createTime  = time.Now()

	nodeCollection         = collections.K8SCoreV1Nodes
	podCollection          = collections.K8SCoreV1Pods
	serviceCollection      = collections.K8SCoreV1Services
	endpointsCollection    = collections.K8SCoreV1Endpoints
	serviceEntryCollection = collections.IstioNetworkingV1Alpha3SyntheticServiceentries
	serviceAnnotations     = resource.StringMap{
		"ak1": "av1",
	}
	serviceLabels = resource.StringMap{
		"lk1": "lv1",
	}
	podLabels = resource.StringMap{
		"pk1": "pv1",
	}
)

func TestInvalidCollectionShouldNotPanic(t *testing.T) {
	rt, src, _, _ := newHandler()
	defer rt.Stop()
	src.Handlers.Handle(event.Event{
		Kind:   event.Added,
		Source: collections.IstioNetworkingV1Alpha3Gateways,
		Resource: &resource.Instance{
			Metadata: resource.Metadata{
				FullName: resource.NewFullName("ns", "svc1"),
				Version:  "123",
			},
		},
	})
}

func TestLifecycle(t *testing.T) {
	expectedVersion := 0
	var service *resource.Instance
	var endpoints *resource.Instance

	stages := []stage{
		{
			name:  "NodeSync",
			event: event.FullSyncFor(collections.K8SCoreV1Nodes),
		},
		{
			name:  "PodSync",
			event: event.FullSyncFor(collections.K8SCoreV1Pods),
		},
		{
			name:  "ServiceSync",
			event: event.FullSyncFor(collections.K8SCoreV1Services),
		},
		{
			name:  "EndpointSync",
			event: event.FullSyncFor(collections.K8SCoreV1Endpoints),
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.acc, 1)
			},
		},
		{
			name: "AddNode",
			event: event.Event{
				Kind:     event.Added,
				Source:   nodeCollection,
				Resource: nodeEntry(),
			},
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.acc, 0)
			},
		},
		{
			name: "AddPod1",
			event: event.Event{
				Kind:     event.Added,
				Source:   podCollection,
				Resource: podEntry(resource.NewFullName(namespace, "pod1"), pod1IP, "sa1"),
			},
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.acc, 0)
			},
		},
		{
			name: "AddPod2",
			event: event.Event{
				Kind:     event.Added,
				Source:   podCollection,
				Resource: podEntry(resource.NewFullName(namespace, "pod2"), pod2IP, "sa2"),
			},
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.acc, 0)
			},
		},
		{
			name: "AddService",
			event: event.Event{
				Kind:     event.Added,
				Source:   serviceCollection,
				Resource: entryForService(serviceName, createTime, "v1"),
			},
			validator: func(ctx pipelineContext) {
				service = entryForService(serviceName, createTime, "v1")

				expectNotifications(ctx.t, ctx.acc, 1)
				expectedVersion++
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					Build()
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.dst, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "UpdateService",
			event: event.Event{
				Kind:     event.Updated,
				Source:   serviceCollection,
				Resource: entryForService(serviceName, createTime, "v2"),
			},
			validator: func(ctx pipelineContext) {
				service = entryForService(serviceName, createTime, "v2")

				expectNotifications(ctx.t, ctx.acc, 1)
				expectedVersion++
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					Build()
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.dst, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "AddEndpoints",
			event: event.Event{
				Kind:   event.Added,
				Source: endpointsCollection,
				Resource: newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v1").
					IPs(pod1IP).
					NotReadyIPs(pod2IP).
					Build(),
			},
			validator: func(ctx pipelineContext) {
				entry := newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v1").
					IPs(pod1IP).
					NotReadyIPs(pod2IP).
					Build()
				endpoints = entry

				expectedVersion++
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					NotReadyIPs(pod2IP).
					Build()
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					IPs(pod1IP).
					ServiceAccounts("sa1").
					PodLabels(podLabels).
					Resolution(networking.ServiceEntry_STATIC).
					Build()
				expectResource(ctx.t, ctx.dst, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "ExpandEndpoints",
			event: event.Event{
				Kind:   event.Updated,
				Source: endpointsCollection,
				Resource: newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v2").
					IPs(pod1IP, pod2IP).
					Build(),
			},
			validator: func(ctx pipelineContext) {
				entry := newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v2").
					IPs(pod1IP, pod2IP).
					Build()
				endpoints = entry

				expectNotifications(ctx.t, ctx.acc, 1)
				expectedVersion++
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					Build()
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					IPs(pod1IP, pod2IP).
					ServiceAccounts("sa1", "sa2").
					PodLabels(podLabels).
					Resolution(networking.ServiceEntry_STATIC).
					Build()
				expectResource(ctx.t, ctx.dst, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "ContractEndpoints",
			event: event.Event{
				Kind:   event.Updated,
				Source: endpointsCollection,
				Resource: newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v3").
					IPs(pod2IP).
					NotReadyIPs(pod1IP).
					Build(),
			},
			validator: func(ctx pipelineContext) {
				entry := newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v3").
					IPs(pod2IP).
					NotReadyIPs(pod1IP).
					Build()
				endpoints = entry

				expectNotifications(ctx.t, ctx.acc, 1)
				expectedVersion++
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					NotReadyIPs(pod1IP).
					Build()
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					IPs(pod2IP).
					ServiceAccounts("sa2").
					PodLabels(podLabels).
					Resolution(networking.ServiceEntry_STATIC).
					Build()
				expectResource(ctx.t, ctx.dst, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "DeleteEndpoints",
			event: event.Event{
				Kind:   event.Deleted,
				Source: endpointsCollection,
				Resource: newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v3").
					IPs(pod2IP).
					Build(),
			},
			validator: func(ctx pipelineContext) {
				endpoints = nil
				expectNotifications(ctx.t, ctx.acc, 1)
				expectedVersion++
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					Build()
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.dst, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "DeleteService",
			event: event.Event{
				Kind:     event.Deleted,
				Source:   serviceCollection,
				Resource: entryForService(serviceName, createTime, "v2"),
			},
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.acc, 1)

				expectedVersion++
				expectEmptySnapshot(ctx.t, ctx.dst, expectedVersion)
			},
		},
	}

	newPipeline(stages).run(t, nil)
}

func TestAddOrder(t *testing.T) {
	initialStages := []stage{
		{
			name:  "NodeSync",
			event: event.FullSyncFor(collections.K8SCoreV1Nodes),
		},
		{
			name:  "PodSync",
			event: event.FullSyncFor(collections.K8SCoreV1Pods),
		},
		{
			name:  "ServiceSync",
			event: event.FullSyncFor(collections.K8SCoreV1Services),
		},
		{
			name:  "EndpointSync",
			event: event.FullSyncFor(collections.K8SCoreV1Endpoints),
		},
	}

	stages := []stage{
		{
			name: "Node",
			event: event.Event{
				Kind:     event.Added,
				Source:   nodeCollection,
				Resource: nodeEntry(),
			},
		},
		{
			name: "Pod",
			event: event.Event{
				Kind:     event.Added,
				Source:   podCollection,
				Resource: podEntry(resource.NewFullName(namespace, "pod1"), pod1IP, "sa1"),
			},
		},
		{
			name: "Service",
			event: event.Event{
				Kind:     event.Added,
				Source:   serviceCollection,
				Resource: entryForService(serviceName, createTime, "v1"),
			},
		},
		{
			name: "Endpoints",
			event: event.Event{
				Kind:   event.Added,
				Source: endpointsCollection,
				Resource: newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v1").
					IPs(pod1IP).
					Build(),
			},
		},
	}

	// Iterate over all permutations of the events
	for _, stageOrder := range getStagePermutations(stages) {
		p := newPipeline(append(initialStages, stageOrder...))
		defer p.rt.Stop()

		var resolution networking.ServiceEntry_Resolution
		t.Run(p.name(), func(t *testing.T) {
			var service *resource.Instance
			var endpoints *resource.Instance
			hasPod := false
			hasNode := false
			expectedVersion := 0

			p.run(t, func(ctx pipelineContext) {
				// Determine whether or not an update is expected.
				entry := ctx.s.event.Resource
				updateExpected := false
				switch ctx.s.name {
				case "Service":
					service = entry
					updateExpected = true
				case "Endpoints":
					endpoints = entry
					updateExpected = service != nil
					resolution = networking.ServiceEntry_STATIC
				case "Pod":
					hasPod = true
					updateExpected = service != nil && endpoints != nil
				case "Node":
					hasNode = true
					updateExpected = service != nil && endpoints != nil && hasPod
				case "EndpointSync":
					expectNotifications(ctx.t, ctx.acc, 1)
					return
				}

				if !updateExpected {
					expectNotifications(ctx.t, ctx.acc, 0)
				} else {
					expectNotifications(ctx.t, ctx.acc, 1)
					expectedVersion++
					expectedMetadata := newMetadataBuilder(service, endpoints).
						CreateTime(createTime).
						Version(expectedVersion).
						Labels(serviceLabels).
						Build()

					seBuilder := newServiceEntryBuilder().ServiceName(serviceName)

					if endpoints != nil {
						seBuilder.IPs(pod1IP)

						if hasPod {
							seBuilder.PodLabels(podLabels).ServiceAccounts("sa1")
							if hasNode {
								seBuilder.Region(region).Zone(zone)
							}
						}
					}
					seBuilder.Resolution(resolution)
					expectedBody := seBuilder.Build()
					expectResource(ctx.t, ctx.dst, expectedVersion, expectedMetadata, expectedBody)
				}
			})
		})
	}
}

func TestDeleteOrder(t *testing.T) {
	stages := []stage{
		{
			name: "Node",
			event: event.Event{
				Kind:     event.Deleted,
				Source:   nodeCollection,
				Resource: nodeEntry(),
			},
		},
		{
			name: "Pod",
			event: event.Event{
				Kind:     event.Deleted,
				Source:   podCollection,
				Resource: podEntry(resource.NewFullName(namespace, "pod1"), pod1IP, "sa1"),
			},
		},
		{
			name: "Endpoints",
			event: event.Event{
				Kind:   event.Deleted,
				Source: endpointsCollection,
				Resource: newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v1").
					IPs(pod1IP).
					Build(),
			},
		},
		{
			name: "Service",
			event: event.Event{
				Kind:     event.Deleted,
				Source:   serviceCollection,
				Resource: entryForService(serviceName, createTime, "v1"),
			},
		},
	}

	// Create the initialization stages, which will add all of the resources we're about to delete.
	initStages := append([]stage{}, stages...)
	for i, s := range initStages {
		s.event.Kind = event.Added
		initStages[i] = s
	}

	syncStages := []stage{
		{
			name:  "NodeSync",
			event: event.FullSyncFor(collections.K8SCoreV1Nodes),
		},
		{
			name:  "PodSync",
			event: event.FullSyncFor(collections.K8SCoreV1Pods),
		},
		{
			name:  "ServiceSync",
			event: event.FullSyncFor(collections.K8SCoreV1Services),
		},
		{
			name:  "EndpointSync",
			event: event.FullSyncFor(collections.K8SCoreV1Endpoints),
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.acc, 1)
			},
		},
	}

	initStages = append(syncStages, initStages...)

	for _, orderedStages := range getStagePermutations(stages) {
		p := newPipeline(orderedStages)
		defer p.rt.Stop()

		var resolution networking.ServiceEntry_Resolution
		t.Run(p.name(), func(t *testing.T) {
			// Add all of the resources to the handler.
			initPipeline := &pipeline{
				stages: initStages,
				rt:     p.rt,
				src:    p.src,
				dst:    p.dst, // Use the same handler
				acc:    p.acc,
			}
			t.Run("Initialize", func(t *testing.T) {
				initPipeline.run(t, nil)
				expectNotifications(t, p.acc, 1)
			})

			t.Run("Delete", func(t *testing.T) {
				entry := entryForService(serviceName, createTime, "v1")
				service := &entry

				entry = newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v1").
					IPs(pod1IP).
					Build()
				endpoints := entry

				hasPod := true
				hasNode := true
				expectedVersion := 1

				// Re-run the pipeline, but deleting the resources.
				p.run(t, func(ctx pipelineContext) {
					// Determine whether or not an update is expected.
					updateExpected := false
					switch ctx.s.name {
					case "Service":
						service = nil
						updateExpected = true
					case "Endpoints":
						endpoints = nil
						updateExpected = service != nil
						resolution = networking.ServiceEntry_NONE
					case "Pod":
						hasPod = false
						updateExpected = service != nil && endpoints != nil
						resolution = networking.ServiceEntry_STATIC
					case "Node":
						hasNode = false
						updateExpected = service != nil && endpoints != nil && hasPod
						resolution = networking.ServiceEntry_STATIC
					}

					if !updateExpected {
						expectNotifications(ctx.t, ctx.acc, 0)
					} else {
						expectNotifications(ctx.t, ctx.acc, 1)

						expectedVersion++
						if service == nil {
							expectEmptySnapshot(t, ctx.dst, expectedVersion)
						} else {
							expectedMetadata := newMetadataBuilder(*service, endpoints).
								CreateTime(createTime).
								Version(expectedVersion).
								Labels(serviceLabels).
								Build()

							seBuilder := newServiceEntryBuilder().ServiceName(serviceName)

							if endpoints != nil {
								seBuilder.IPs(pod1IP)

								if hasPod {
									seBuilder.PodLabels(podLabels).ServiceAccounts("sa1")
									if hasNode {
										seBuilder.Region(region).Zone(zone)
									}
								}
							}
							seBuilder.Resolution(resolution)
							expectedBody := seBuilder.Build()
							expectResource(ctx.t, ctx.dst, expectedVersion, expectedMetadata, expectedBody)
						}
					}
				})
			})
		})
	}
}

func TestReceiveEndpointsBeforeService(t *testing.T) {
	rt, src, dst, acc := newHandler()
	defer rt.Stop()

	syncEvents := []event.Event{
		event.FullSyncFor(collections.K8SCoreV1Nodes),
		event.FullSyncFor(collections.K8SCoreV1Pods),
		event.FullSyncFor(collections.K8SCoreV1Services),
		event.FullSyncFor(collections.K8SCoreV1Endpoints),
	}

	for _, e := range syncEvents {
		src.Handlers.Handle(e)
	}
	expectNotifications(t, acc, 1)

	expectedVersion := 0
	t.Run("AddNode", func(t *testing.T) {
		src.Handlers.Handle(event.Event{
			Kind:     event.Added,
			Source:   nodeCollection,
			Resource: nodeEntry(),
		})
		expectNotifications(t, acc, 0)
	})

	t.Run("AddPod", func(t *testing.T) {
		src.Handlers.Handle(event.Event{
			Kind:     event.Added,
			Source:   podCollection,
			Resource: podEntry(resource.NewFullName(namespace, "pod1"), pod1IP, "sa1"),
		})
		expectNotifications(t, acc, 0)
	})

	var endpoints *resource.Instance
	t.Run("AddEndpoints", func(t *testing.T) {
		endpoints = newEndpointsEntryBuilder().
			ServiceName(serviceName).
			CreateTime(createTime).
			Version("v1").
			IPs(pod1IP).
			Build()
		src.Handlers.Handle(event.Event{
			Kind:     event.Added,
			Source:   endpointsCollection,
			Resource: endpoints,
		})
		expectNotifications(t, acc, 0)
		expectEmptySnapshot(t, dst, expectedVersion)
	})

	t.Run("AddService", func(t *testing.T) {
		service := entryForService(serviceName, createTime, "v1")
		src.Handlers.Handle(event.Event{
			Kind:     event.Added,
			Source:   serviceCollection,
			Resource: service,
		})
		expectNotifications(t, acc, 1)
		expectedVersion++
		expectedMetadata := newMetadataBuilder(service, endpoints).
			CreateTime(createTime).
			Version(expectedVersion).
			Labels(serviceLabels).
			Build()
		expectedBody := newServiceEntryBuilder().
			ServiceName(serviceName).
			Region(region).
			Zone(zone).
			IPs(pod1IP).
			ServiceAccounts("sa1").
			PodLabels(podLabels).
			Resolution(networking.ServiceEntry_STATIC).
			Build()
		expectResource(t, dst, expectedVersion, expectedMetadata, expectedBody)
	})
}

func TestAddEndpointsWithUnknownEventKindShouldNotPanic(t *testing.T) {
	rt, src, _, acc := newHandler()
	defer rt.Stop()

	src.Handlers.Handle(event.Event{
		Kind: event.None,
		Resource: newEndpointsEntryBuilder().
			ServiceName(serviceName).
			CreateTime(createTime).
			Version("v1").
			IPs(pod1IP).
			Build(),
	})
	expectNotifications(t, acc, 0)
}

func newHandler() (*processing.Runtime, *fixtures.Source, *snapshotter.InMemoryDistributor, *fixtures.Accumulator) {
	a := &fixtures.Accumulator{}

	src := &fixtures.Source{}
	meshSrc := meshcfg.NewInmemory()
	meshSrc.Set(meshcfg.Default())

	dst := snapshotter.NewInMemoryDistributor()
	o := processing.RuntimeOptions{
		DomainSuffix: domain,
		Source:       event.CombineSources(src, meshSrc),
		ProcessorProvider: func(o processing.ProcessorOptions) event.Processor {
			xforms := serviceentry.GetProviders().Create(o)
			xforms[0].DispatchFor(collections.IstioNetworkingV1Alpha3SyntheticServiceentries, a)
			settings := []snapshotter.SnapshotOptions{
				{
					Group:       "syntheticServiceEntry",
					Collections: []collection.Name{collections.IstioNetworkingV1Alpha3SyntheticServiceentries.Name()},
					Strategy:    strategy.NewImmediate(),
					Distributor: dst,
				},
			}
			s, _ := snapshotter.NewSnapshotter(xforms, settings)
			return s
		},
	}
	p := processing.NewRuntime(o)
	p.Start()

	return p, src, dst, a
}

func nodeEntry() *resource.Instance {
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName: resource.NewFullName("", nodeName),
			Version:  "v1",
			Labels:   localityLabels(region, zone),
		},
		Message: &coreV1.NodeSpec{},
	}
}

func podEntry(podName resource.FullName, ip, saName string) *resource.Instance {
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName: podName,
			Version:  "v1",
		},
		Message: &coreV1.Pod{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      podName.Name.String(),
				Namespace: podName.Namespace.String(),
				Labels:    podLabels,
			},
			Spec: coreV1.PodSpec{
				NodeName:           nodeName,
				ServiceAccountName: saName,
			},
			Status: coreV1.PodStatus{
				PodIP: ip,
				Phase: coreV1.PodRunning,
			},
		},
	}
}

func entryForService(serviceName resource.FullName, createTime time.Time, version string) *resource.Instance {
	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName:    serviceName,
			Version:     resource.Version(version),
			CreateTime:  createTime,
			Annotations: serviceAnnotations,
			Labels:      serviceLabels,
		},
		Message: &coreV1.ServiceSpec{
			ClusterIP: clusterIP,
			Ports: []coreV1.ServicePort{
				{
					Name:     "http",
					Protocol: coreV1.ProtocolTCP,
					Port:     80,
				},
			},
		},
	}
}

type endpointsEntryBuilder struct {
	serviceName resource.FullName
	createTime  time.Time
	version     string
	ips         []string
	notReadyIPs []string
}

func newEndpointsEntryBuilder() *endpointsEntryBuilder {
	return &endpointsEntryBuilder{}
}

func (b *endpointsEntryBuilder) ServiceName(serviceName resource.FullName) *endpointsEntryBuilder {
	b.serviceName = serviceName
	return b
}

func (b *endpointsEntryBuilder) CreateTime(createTime time.Time) *endpointsEntryBuilder {
	b.createTime = createTime
	return b
}

func (b *endpointsEntryBuilder) Version(version string) *endpointsEntryBuilder {
	b.version = version
	return b
}

func (b *endpointsEntryBuilder) IPs(ips ...string) *endpointsEntryBuilder {
	b.ips = ips
	return b
}

func (b *endpointsEntryBuilder) NotReadyIPs(ips ...string) *endpointsEntryBuilder {
	b.notReadyIPs = ips
	return b
}

func (b *endpointsEntryBuilder) Build() *resource.Instance {
	eps := &coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			CreationTimestamp: metaV1.Time{Time: b.createTime},
			Name:              b.serviceName.Name.String(),
			Namespace:         b.serviceName.Namespace.String(),
		},
		Subsets: []coreV1.EndpointSubset{
			{
				Ports: []coreV1.EndpointPort{
					{
						Name:     "http",
						Port:     80,
						Protocol: coreV1.ProtocolTCP,
					},
				},
			},
		},
	}

	for _, ip := range b.ips {
		eps.Subsets[0].Addresses = append(eps.Subsets[0].Addresses, coreV1.EndpointAddress{
			IP: ip,
		})
	}

	for _, ip := range b.notReadyIPs {
		eps.Subsets[0].NotReadyAddresses = append(eps.Subsets[0].NotReadyAddresses, coreV1.EndpointAddress{
			IP: ip,
		})
	}

	return &resource.Instance{
		Metadata: resource.Metadata{
			FullName:    b.serviceName,
			Version:     resource.Version(b.version),
			CreateTime:  b.createTime,
			Annotations: serviceAnnotations,
		},
		Message: eps,
	}
}

func host(namespace, serviceName string) string {
	return fmt.Sprintf("%s.%s.svc.%s", serviceName, namespace, domain)
}

func localityLabels(region, zone string) resource.StringMap {
	labels := make(resource.StringMap)
	if region != "" {
		labels[pod.LabelZoneRegion] = region
	}
	if zone != "" {
		labels[pod.LabelZoneFailureDomain] = zone
	}
	return labels
}

type metadataBuilder struct {
	service     *resource.Instance
	endpoints   *resource.Instance
	notReadyIPs []string

	version    int
	createTime time.Time
	labels     map[string]string
}

func newMetadataBuilder(service *resource.Instance, endpoints *resource.Instance) *metadataBuilder {
	return &metadataBuilder{
		service:   service,
		endpoints: endpoints,
	}
}

func (b *metadataBuilder) NotReadyIPs(notReadyIPs ...string) *metadataBuilder {
	b.notReadyIPs = notReadyIPs
	return b
}

func (b *metadataBuilder) Version(version int) *metadataBuilder {
	b.version = version
	return b
}

func (b *metadataBuilder) CreateTime(createTime time.Time) *metadataBuilder {
	b.createTime = createTime
	return b
}

func (b *metadataBuilder) Labels(labels map[string]string) *metadataBuilder {
	b.labels = labels
	return b
}

func (b *metadataBuilder) Build() *mcp.Metadata {
	protoTime, _ := types.TimestampProto(b.createTime)

	annos := make(map[string]string)
	for k, v := range b.service.Metadata.Annotations {
		annos[k] = v
	}
	annos[annotation.AlphaNetworkingServiceVersion.Name] = string(b.service.Metadata.Version)
	if b.endpoints != nil {
		annos[annotation.AlphaNetworkingEndpointsVersion.Name] = string(b.endpoints.Metadata.Version)
		if len(b.notReadyIPs) > 0 {
			annos[annotation.AlphaNetworkingNotReadyEndpoints.Name] = notReadyAnnotation(b.notReadyIPs...)
		}
	}

	return &mcp.Metadata{
		Name:        serviceName.String(),
		Version:     strconv.Itoa(b.version),
		CreateTime:  protoTime,
		Labels:      b.labels,
		Annotations: annos,
	}
}

type serviceEntryBuilder struct {
	serviceName     resource.FullName
	region          string
	zone            string
	ips             []string
	serviceAccounts []string
	podLabels       map[string]string
	resolution      networking.ServiceEntry_Resolution
}

func newServiceEntryBuilder() *serviceEntryBuilder {
	return &serviceEntryBuilder{}
}

func (b *serviceEntryBuilder) ServiceName(serviceName resource.FullName) *serviceEntryBuilder {
	b.serviceName = serviceName
	return b
}

func (b *serviceEntryBuilder) Region(region string) *serviceEntryBuilder {
	b.region = region
	return b
}

func (b *serviceEntryBuilder) Zone(zone string) *serviceEntryBuilder {
	b.zone = zone
	return b
}

func (b *serviceEntryBuilder) IPs(ips ...string) *serviceEntryBuilder {
	b.ips = ips
	return b
}

func (b *serviceEntryBuilder) ServiceAccounts(serviceAccounts ...string) *serviceEntryBuilder {
	b.serviceAccounts = serviceAccounts
	return b
}

func (b *serviceEntryBuilder) PodLabels(podLabels map[string]string) *serviceEntryBuilder {
	b.podLabels = podLabels
	return b
}

func (b *serviceEntryBuilder) Resolution(res networking.ServiceEntry_Resolution) *serviceEntryBuilder {
	b.resolution = res
	return b
}

func (b *serviceEntryBuilder) Build() *networking.ServiceEntry {
	entry := &networking.ServiceEntry{
		Hosts:      []string{host(b.serviceName.Namespace.String(), b.serviceName.Name.String())},
		Addresses:  []string{clusterIP},
		Resolution: b.resolution,
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Ports: []*networking.Port{
			{
				Name:     "http",
				Number:   80,
				Protocol: string(protocol.HTTP),
			},
		},
		SubjectAltNames: expectedSubjectAltNames(b.serviceName.Namespace, b.serviceAccounts),
	}

	for _, ip := range b.ips {
		entry.Endpoints = append(entry.Endpoints, &networking.ServiceEntry_Endpoint{
			Address: ip,
			Ports: map[string]uint32{
				"http": 80,
			},

			Locality: localityFor(b.region, b.zone),
			Labels:   b.podLabels,
		})
	}

	return entry
}

type validatorFunc func(ctx pipelineContext)

type stage struct {
	name      string
	event     event.Event
	validator validatorFunc
}

type pipelineContext struct {
	t   *testing.T
	acc *fixtures.Accumulator
	src *fixtures.Source
	dst *snapshotter.InMemoryDistributor
	s   stage
}

type pipeline struct {
	stages []stage
	rt     *processing.Runtime
	acc    *fixtures.Accumulator
	src    *fixtures.Source
	dst    *snapshotter.InMemoryDistributor
}

func newPipeline(stages []stage) *pipeline {
	rt, src, dst, acc := newHandler()
	return &pipeline{
		stages: append([]stage{}, stages...),
		rt:     rt,
		src:    src,
		dst:    dst,
		acc:    acc,
	}
}

func (p *pipeline) name() string {
	name := ""
	for i, s := range p.stages {
		if i > 0 {
			name += "_"
		}
		name += s.name
	}
	return name
}

func (p *pipeline) run(t *testing.T, globalValidator validatorFunc) {
	t.Helper()
	failed := false
	for _, s := range p.stages {
		success := t.Run(s.name, func(t *testing.T) {
			if failed {
				t.Fatal("previous stage failed")
			}

			// Clear the accumulator
			p.acc.Clear()

			// Handle the event.
			p.src.Handlers.Handle(s.event)

			// If a global validator was supplied, use it. Otherwise use the stage validator.
			v := globalValidator
			if v == nil {
				v = s.validator
			}
			if v != nil {
				v(pipelineContext{
					t:   t,
					src: p.src,
					dst: p.dst,
					acc: p.acc,
					s:   s,
				})
			}
		})
		failed = failed || !success
	}
}

func getStagePermutations(values []stage) [][]stage {
	var helper func([]stage, int)
	res := make([][]stage, 0)

	helper = func(arr []stage, n int) {
		if n == 1 {
			tmp := make([]stage, len(arr))
			copy(tmp, arr)
			res = append(res, tmp)
		} else {
			for i := 0; i < n; i++ {
				helper(arr, n-1)
				if n%2 == 1 {
					arr[i], arr[n-1] = arr[n-1], arr[i]
				} else {
					arr[0], arr[n-1] = arr[n-1], arr[0]
				}
			}
		}
	}
	helper(values, len(values))
	return res
}

func localityFor(region, zone string) string {
	if region != "" || zone != "" {
		return fmt.Sprintf("%s/%s", region, zone)
	}
	return ""
}

func notReadyAnnotation(ips ...string) string {
	for i := range ips {
		ips[i] += ":80"
	}

	return strings.Join(ips, ",")
}

func expectNotifications(t *testing.T, a *fixtures.Accumulator, count int) {
	t.Helper()
	g := NewGomegaWithT(t)

	if count == 0 {
		g.Consistently(a.Events).Should(HaveLen(count))
	} else {
		g.Eventually(a.Events).Should(HaveLen(count))
	}
	for _, e := range a.Events() {
		g.Expect(e.Source).To(Equal(serviceEntryCollection))
	}
	a.Clear()
}

func toJSON(t *testing.T, obj interface{}) string {
	t.Helper()
	if obj == nil {
		return "nil"
	}

	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func expectResource(
	t *testing.T,
	dst *snapshotter.InMemoryDistributor,
	expectedVersion int,
	expectedMetadata *mcp.Metadata,
	expectedBody *networking.ServiceEntry) {
	t.Helper()
	g := NewGomegaWithT(t)

	g.Eventually(func() snapshot.Snapshot {
		return dst.GetSnapshot("syntheticServiceEntry")
	}).ShouldNot(BeNil())

	expectedVersionStr := fmt.Sprintf("istio/networking/v1alpha3/synthetic/serviceentries/%d", expectedVersion)
	g.Eventually(func() string {
		sn := dst.GetSnapshot("syntheticServiceEntry")
		return sn.Version(serviceEntryCollection.Name().String())
	}).Should(Equal(expectedVersionStr))

	sn := dst.GetSnapshot("syntheticServiceEntry")
	// Extract out the resource.
	rs := sn.Resources(serviceEntryCollection.Name().String())
	if len(rs) != 1 {
		t.Fatalf("expected snapshot resource count %d to equal %d", len(rs), 1)
	}
	actual := rs[0]

	// Verify the content.
	actualMetadata := actual.Metadata
	actualBody := &networking.ServiceEntry{}
	if err := types.UnmarshalAny(actual.Body, actualBody); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualMetadata, expectedMetadata) {
		t.Fatalf("expected:\n%s\nto equal:\n%s\n", toJSON(t, actualMetadata), toJSON(t, expectedMetadata))
	}
	if !reflect.DeepEqual(actualBody, expectedBody) {
		t.Fatalf("expected:\n%s\nto equal:\n%s\n", toJSON(t, actualBody), toJSON(t, expectedBody))
	}
}

func expectEmptySnapshot(t *testing.T, dst *snapshotter.InMemoryDistributor, expectedVersion int) {
	t.Helper()
	g := NewGomegaWithT(t)

	g.Eventually(func() snapshot.Snapshot { return dst.GetSnapshot("syntheticServiceEntry") }).ShouldNot(BeNil())
	expectedVersionStr := fmt.Sprintf("istio/networking/v1alpha3/synthetic/serviceentries/%d", expectedVersion)
	g.Eventually(func() string {
		sn := dst.GetSnapshot("syntheticServiceEntry")
		return sn.Version(serviceEntryCollection.Name().String())
	}).Should(Equal(expectedVersionStr))

	sn := dst.GetSnapshot("syntheticServiceEntry")

	// Verify there are no resources in the snapshot.
	rs := sn.Resources(serviceEntryCollection.Name().String())
	if len(rs) != 0 {
		t.Fatalf("expected snapshot resource count %d to equal %d", len(rs), 0)
	}
}

func expectedSubjectAltNames(ns resource.Namespace, serviceAccountNames []string) []string {
	if serviceAccountNames == nil {
		return nil
	}
	out := make([]string, 0, len(serviceAccountNames))
	for _, serviceAccountName := range serviceAccountNames {
		out = append(out, expectedSubjectAltName(ns, serviceAccountName))
	}
	return out
}

func expectedSubjectAltName(ns resource.Namespace, serviceAccountName string) string {
	return fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/%s", ns, serviceAccountName)
}
