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

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/annotations"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis"
)

const (
	domain    = "company.com"
	clusterIP = "10.0.0.10"
	pod1IP    = "10.0.0.1"
	pod2IP    = "10.0.0.2"
	namespace = "fakeNamespace"
	nodeName  = "node1"
	region    = "region1"
	zone      = "zone1"
)

var (
	serviceName = name(namespace, "svc1")
	createTime  = time.Now()

	nodeCollection         = metadata.K8sCoreV1Nodes.Collection
	podCollection          = metadata.K8sCoreV1Pods.Collection
	serviceCollection      = metadata.K8sCoreV1Services.Collection
	endpointsCollection    = metadata.K8sCoreV1Endpoints.Collection
	serviceEntryCollection = metadata.IstioNetworkingV1alpha3SyntheticServiceentries.Collection
	serviceAnnotations     = resource.Annotations{
		"ak1": "av1",
	}
	serviceLabels = resource.Labels{
		"lk1": "lv1",
	}
	podLabels = resource.Labels{
		"pk1": "pv1",
	}
)

func TestInvalidCollectionShouldNotPanic(t *testing.T) {
	h, _ := newHandler()
	h.Handle(resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: id(metadata.IstioNetworkingV1alpha3Gateways.Collection, name("ns", "svc1"), "123"),
		},
	})
}

func TestLifecycle(t *testing.T) {
	expectedVersion := 0
	var service resource.Entry
	var endpoints *resource.Entry

	stages := []stage{
		{
			name: "AddNode",
			event: resource.Event{
				Kind:  resource.Added,
				Entry: nodeEntry(),
			},
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.l, 0)
			},
		},
		{
			name: "AddPod1",
			event: resource.Event{
				Kind:  resource.Added,
				Entry: podEntry(name(namespace, "pod1"), pod1IP, "sa1"),
			},
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.l, 0)
			},
		},
		{
			name: "AddPod2",
			event: resource.Event{
				Kind:  resource.Added,
				Entry: podEntry(name(namespace, "pod2"), pod2IP, "sa2"),
			},
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.l, 0)
			},
		},
		{
			name: "AddService",
			event: resource.Event{
				Kind:  resource.Added,
				Entry: entryForService(serviceName, createTime, "v1"),
			},
			validator: func(ctx pipelineContext) {
				service = entryForService(serviceName, createTime, "v1")

				expectNotifications(ctx.t, ctx.l, 1)
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					Build()
				expectedVersion++
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.h, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "UpdateService",
			event: resource.Event{
				Kind:  resource.Updated,
				Entry: entryForService(serviceName, createTime, "v2"),
			},
			validator: func(ctx pipelineContext) {
				service = entryForService(serviceName, createTime, "v2")

				expectNotifications(ctx.t, ctx.l, 1)
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					Build()
				expectedVersion++
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.h, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "AddEndpoints",
			event: resource.Event{
				Kind: resource.Added,
				Entry: newEndpointsEntryBuilder().
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
				endpoints = &entry

				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					NotReadyIPs(pod2IP).
					Build()
				expectedVersion++
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					IPs(pod1IP).
					ServiceAccounts("sa1").
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.h, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "ExpandEndpoints",
			event: resource.Event{
				Kind: resource.Updated,
				Entry: newEndpointsEntryBuilder().
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
				endpoints = &entry

				expectNotifications(ctx.t, ctx.l, 1)
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					Build()
				expectedVersion++
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					IPs(pod1IP, pod2IP).
					ServiceAccounts("sa1", "sa2").
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.h, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "ContractEndpoints",
			event: resource.Event{
				Kind: resource.Updated,
				Entry: newEndpointsEntryBuilder().
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
				endpoints = &entry

				expectNotifications(ctx.t, ctx.l, 1)
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					NotReadyIPs(pod1IP).
					Build()
				expectedVersion++
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					IPs(pod2IP).
					ServiceAccounts("sa2").
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.h, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "DeleteEndpoints",
			event: resource.Event{
				Kind: resource.Deleted,
				Entry: newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v3").
					IPs(pod2IP).
					Build(),
			},
			validator: func(ctx pipelineContext) {
				endpoints = nil
				expectNotifications(ctx.t, ctx.l, 1)
				expectedMetadata := newMetadataBuilder(service, endpoints).
					CreateTime(createTime).
					Version(expectedVersion).
					Labels(serviceLabels).
					Build()
				expectedVersion++
				expectedBody := newServiceEntryBuilder().
					ServiceName(serviceName).
					Region(region).
					Zone(zone).
					PodLabels(podLabels).
					Build()
				expectResource(ctx.t, ctx.h, expectedVersion, expectedMetadata, expectedBody)
			},
		},
		{
			name: "DeleteService",
			event: resource.Event{
				Kind:  resource.Deleted,
				Entry: entryForService(serviceName, createTime, "v2"),
			},
			validator: func(ctx pipelineContext) {
				expectNotifications(ctx.t, ctx.l, 1)

				expectedVersion++
				expectEmptySnapshot(ctx.t, ctx.h, expectedVersion)
			},
		},
	}

	newPipeline(stages).run(t, nil)
}

func TestAddOrder(t *testing.T) {
	stages := []stage{
		{
			name: "Node",
			event: resource.Event{
				Kind:  resource.Added,
				Entry: nodeEntry(),
			},
		},
		{
			name: "Pod",
			event: resource.Event{
				Kind:  resource.Added,
				Entry: podEntry(name(namespace, "pod1"), pod1IP, "sa1"),
			},
		},
		{
			name: "Service",
			event: resource.Event{
				Kind:  resource.Added,
				Entry: entryForService(serviceName, createTime, "v1"),
			},
		},
		{
			name: "Endpoints",
			event: resource.Event{
				Kind: resource.Added,
				Entry: newEndpointsEntryBuilder().
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
		p := newPipeline(stageOrder)
		t.Run(p.name(), func(t *testing.T) {
			var service *resource.Entry
			var endpoints *resource.Entry
			hasPod := false
			hasNode := false
			expectedVersion := 0

			p.run(t, func(ctx pipelineContext) {
				// Determine whether or not an update is expected.
				entry := ctx.s.event.Entry
				updateExpected := false
				switch ctx.s.name {
				case "Service":
					service = &entry
					updateExpected = true
				case "Endpoints":
					endpoints = &entry
					updateExpected = service != nil
				case "Pod":
					hasPod = true
					updateExpected = service != nil && endpoints != nil
				case "Node":
					hasNode = true
					updateExpected = service != nil && endpoints != nil && hasPod
				}

				if !updateExpected {
					expectNotifications(ctx.t, ctx.l, 0)
				} else {
					expectNotifications(ctx.t, ctx.l, 1)
					expectedMetadata := newMetadataBuilder(*service, endpoints).
						CreateTime(createTime).
						Version(expectedVersion).
						Labels(serviceLabels).
						Build()
					expectedVersion++

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

					expectedBody := seBuilder.Build()
					expectResource(ctx.t, ctx.h, expectedVersion, expectedMetadata, expectedBody)
				}
			})
		})
	}
}

func TestDeleteOrder(t *testing.T) {
	stages := []stage{
		{
			name: "Node",
			event: resource.Event{
				Kind:  resource.Deleted,
				Entry: nodeEntry(),
			},
		},
		{
			name: "Pod",
			event: resource.Event{
				Kind:  resource.Deleted,
				Entry: podEntry(name(namespace, "pod1"), pod1IP, "sa1"),
			},
		},
		{
			name: "Service",
			event: resource.Event{
				Kind:  resource.Deleted,
				Entry: entryForService(serviceName, createTime, "v1"),
			},
		},
		{
			name: "Endpoints",
			event: resource.Event{
				Kind: resource.Deleted,
				Entry: newEndpointsEntryBuilder().
					ServiceName(serviceName).
					CreateTime(createTime).
					Version("v1").
					IPs(pod1IP).
					Build(),
			},
		},
	}

	// Create the initialization stages, which will add all of the resources we're about to delete.
	initStages := append([]stage{}, stages...)
	for i, s := range initStages {
		s.event.Kind = resource.Added
		initStages[i] = s
	}

	for _, orderedStages := range getStagePermutations(stages) {
		p := newPipeline(orderedStages)

		t.Run(p.name(), func(t *testing.T) {
			// Add all of the resources to the handler.
			initPipeline := &pipeline{
				stages: initStages,
				h:      p.h, // Use the same handler
				l:      p.l,
			}
			t.Run("Initialize", func(t *testing.T) {
				initPipeline.run(t, nil)
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
				endpoints := &entry

				hasPod := true
				hasNode := true
				expectedVersion := 2

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
					case "Pod":
						hasPod = false
						updateExpected = service != nil && endpoints != nil
					case "Node":
						hasNode = false
						updateExpected = service != nil && endpoints != nil && hasPod
					}

					if !updateExpected {
						expectNotifications(ctx.t, ctx.l, 0)
					} else {
						expectNotifications(ctx.t, ctx.l, 1)

						if service == nil {
							expectedVersion++
							expectEmptySnapshot(t, ctx.h, expectedVersion)
						} else {
							expectedMetadata := newMetadataBuilder(*service, endpoints).
								CreateTime(createTime).
								Version(expectedVersion).
								Labels(serviceLabels).
								Build()
							expectedVersion++

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

							expectedBody := seBuilder.Build()
							expectResource(ctx.t, ctx.h, expectedVersion, expectedMetadata, expectedBody)
						}
					}
				})
			})
		})
	}
}

func TestReceiveEndpointsBeforeService(t *testing.T) {
	h, l := newHandler()

	expectedVersion := 0
	t.Run("AddNode", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: nodeEntry(),
		})
		expectNotifications(t, l, 0)
	})

	t.Run("AddPod", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: podEntry(name(namespace, "pod1"), pod1IP, "sa1"),
		})
		expectNotifications(t, l, 0)
	})

	var endpoints resource.Entry
	t.Run("AddEndpoints", func(t *testing.T) {
		endpoints = newEndpointsEntryBuilder().
			ServiceName(serviceName).
			CreateTime(createTime).
			Version("v1").
			IPs(pod1IP).
			Build()
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: endpoints,
		})
		expectNotifications(t, l, 0)
		expectEmptySnapshot(t, h, expectedVersion)
	})

	t.Run("AddService", func(t *testing.T) {
		service := entryForService(serviceName, createTime, "v1")
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: service,
		})
		expectNotifications(t, l, 1)
		expectedMetadata := newMetadataBuilder(service, &endpoints).
			CreateTime(createTime).
			Version(expectedVersion).
			Labels(serviceLabels).
			Build()
		expectedVersion++
		expectedBody := newServiceEntryBuilder().
			ServiceName(serviceName).
			Region(region).
			Zone(zone).
			IPs(pod1IP).
			ServiceAccounts("sa1").
			PodLabels(podLabels).
			Build()
		expectResource(t, h, expectedVersion, expectedMetadata, expectedBody)
	})
}

func TestAddServiceWithUnknownEventKindShouldNotPanic(t *testing.T) {
	h, l := newHandler()

	h.Handle(resource.Event{
		Kind:  resource.None,
		Entry: entryForService(serviceName, createTime, "v1"),
	})
	expectNotifications(t, l, 0)
}

func TestAddEndpointsWithUnknownEventKindShouldNotPanic(t *testing.T) {
	h, l := newHandler()

	h.Handle(resource.Event{
		Kind: resource.None,
		Entry: newEndpointsEntryBuilder().
			ServiceName(serviceName).
			CreateTime(createTime).
			Version("v1").
			IPs(pod1IP).
			Build(),
	})
	expectNotifications(t, l, 0)
}

var _ processing.Listener = &fakeListener{}

type fakeListener struct {
	changed []resource.Collection
}

func (l *fakeListener) CollectionChanged(c resource.Collection) {
	l.changed = append(l.changed, c)
}

func (l *fakeListener) reset() {
	l.changed = l.changed[:0]
}

func newHandler() (*serviceentry.Handler, *fakeListener) {
	l := &fakeListener{}
	h := serviceentry.NewHandler(domain, l)
	return h, l
}

func name(ns, name string) resource.FullName {
	return resource.FullNameFromNamespaceAndName(ns, name)
}

func id(c resource.Collection, name resource.FullName, version string) resource.VersionedKey {
	return resource.VersionedKey{
		Key: resource.Key{
			Collection: c,
			FullName:   name,
		},
		Version: resource.Version(version),
	}
}

func nodeEntry() resource.Entry {
	return resource.Entry{
		ID: id(nodeCollection, name("", nodeName), "v1"),
		Metadata: resource.Metadata{
			Labels: localityLabels(region, zone),
		},
		Item: &coreV1.NodeSpec{},
	}
}

func podEntry(podName resource.FullName, ip, saName string) resource.Entry {
	ns, name := podName.InterpretAsNamespaceAndName()
	return resource.Entry{
		ID: id(podCollection, podName, "v1"),
		Item: &coreV1.Pod{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name,
				Namespace: ns,
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

func entryForService(serviceName resource.FullName, createTime time.Time, version string) resource.Entry {
	return resource.Entry{
		ID: id(serviceCollection, serviceName, version),
		Metadata: resource.Metadata{
			CreateTime:  createTime,
			Annotations: serviceAnnotations,
			Labels:      serviceLabels,
		},
		Item: &coreV1.ServiceSpec{
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

func (b *endpointsEntryBuilder) Build() resource.Entry {
	ns, n := b.serviceName.InterpretAsNamespaceAndName()

	eps := &coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			CreationTimestamp: metaV1.Time{Time: b.createTime},
			Name:              n,
			Namespace:         ns,
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

	return resource.Entry{
		ID: id(endpointsCollection, b.serviceName, b.version),
		Metadata: resource.Metadata{
			CreateTime:  b.createTime,
			Annotations: serviceAnnotations,
		},
		Item: eps,
	}
}

func host(namespace, serviceName string) string {
	return fmt.Sprintf("%s.%s.svc.%s", serviceName, namespace, domain)
}

func localityLabels(region, zone string) resource.Labels {
	labels := make(resource.Labels)
	if region != "" {
		labels[apis.LabelZoneRegion] = region
	}
	if zone != "" {
		labels[apis.LabelZoneFailureDomain] = zone
	}
	return labels
}

type metadataBuilder struct {
	service     resource.Entry
	endpoints   *resource.Entry
	notReadyIPs []string

	version    int
	createTime time.Time
	labels     map[string]string
}

func newMetadataBuilder(service resource.Entry, endpoints *resource.Entry) *metadataBuilder {
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
	annos[annotations.ServiceVersion] = string(b.service.ID.Version)
	if b.endpoints != nil {
		annos[annotations.EndpointsVersion] = string(b.endpoints.ID.Version)
		if len(b.notReadyIPs) > 0 {
			annos[annotations.NotReadyEndpoints] = notReadyAnnotation(b.notReadyIPs...)
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

func (b *serviceEntryBuilder) Build() *networking.ServiceEntry {
	ns, n := b.serviceName.InterpretAsNamespaceAndName()
	entry := &networking.ServiceEntry{
		Hosts:      []string{host(ns, n)},
		Addresses:  []string{clusterIP},
		Resolution: networking.ServiceEntry_STATIC,
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Ports: []*networking.Port{
			{
				Name:     "http",
				Number:   80,
				Protocol: string(model.ProtocolHTTP),
			},
		},
		SubjectAltNames: expectedSubjectAltNames(ns, b.serviceAccounts),
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
	event     resource.Event
	validator validatorFunc
}

type pipelineContext struct {
	t *testing.T
	h *serviceentry.Handler
	l *fakeListener
	s stage
}

type pipeline struct {
	stages []stage
	h      *serviceentry.Handler
	l      *fakeListener
}

func newPipeline(stages []stage) *pipeline {
	h, l := newHandler()
	return &pipeline{
		stages: append([]stage{}, stages...),
		h:      h,
		l:      l,
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

			// Reset the listener.
			p.l.reset()

			// Handle the event.
			p.h.Handle(s.event)

			// If a global validator was supplied, use it. Otherwise use the stage validator.
			v := globalValidator
			if v == nil {
				v = s.validator
			}
			if v != nil {
				v(pipelineContext{
					t: t,
					h: p.h,
					l: p.l,
					s: s,
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

func expectNotifications(t *testing.T, l *fakeListener, count int) {
	t.Helper()
	if len(l.changed) != count {
		t.Fatalf("expected %d notifications, found %d", count, len(l.changed))
	}
	for i, col := range l.changed {
		if col != serviceEntryCollection {
			t.Fatalf("expected event[%d]=%s to equal %s", i, toJSON(t, col), toJSON(t, serviceEntryCollection))
		}
	}
	l.reset()
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

func expectResource(t *testing.T, h *serviceentry.Handler, expectedVersion int, expectedMetadata *mcp.Metadata, expectedBody *networking.ServiceEntry) {
	t.Helper()
	s := h.BuildSnapshot()

	// Verify the version.
	actualVersion := s.Version(serviceEntryCollection.String())
	expectedVersionStr := strconv.Itoa(expectedVersion)
	if actualVersion != expectedVersionStr {
		t.Fatalf("expected snapshot version %s to equal %s", actualVersion, expectedVersionStr)
	}

	// Extract out the resource.
	rs := s.Resources(serviceEntryCollection.String())
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

func expectEmptySnapshot(t *testing.T, h *serviceentry.Handler, expectedVersion int) {
	s := h.BuildSnapshot()

	// Verify the version changed.
	actualVersion := s.Version(serviceEntryCollection.String())
	expectedVersionStr := strconv.Itoa(expectedVersion)
	if actualVersion != expectedVersionStr {
		t.Fatalf("expected snapshot version %s to equal %s", actualVersion, expectedVersionStr)
	}

	// Verify there are no resources in the snapshot.
	rs := s.Resources(serviceEntryCollection.String())
	if len(rs) != 0 {
		t.Fatalf("expected snapshot resource count %d to equal %d", len(rs), 0)
	}
}

func expectedSubjectAltNames(ns string, serviceAccountNames []string) []string {
	if serviceAccountNames == nil {
		return nil
	}
	out := make([]string, 0, len(serviceAccountNames))
	for _, serviceAccountName := range serviceAccountNames {
		out = append(out, expectedSubjectAltName(ns, serviceAccountName))
	}
	return out
}

func expectedSubjectAltName(ns, serviceAccountName string) string {
	return fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/%s", ns, serviceAccountName)
}
