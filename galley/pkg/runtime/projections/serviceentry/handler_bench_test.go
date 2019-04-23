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
	"strconv"
	"testing"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry"
	"istio.io/istio/galley/pkg/runtime/resource"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis"
)

var (
	ips = []string{
		"10.0.0.1",
		"10.0.0.2",
		"10.0.0.3",
		"10.0.0.4",
		"10.0.0.5",
		"10.0.0.6",
		"10.0.0.7",
		"10.0.0.8",
		"10.0.0.9",
		"10.0.0.10",
	}
	serviceAccounts = []string{
		"serviceAccount1",
		"serviceAccount2",
		"serviceAccount3",
	}
	annos = resource.Annotations{
		"Annotation1": "AnnotationValue1",
		"Annotation2": "AnnotationValue2",
		"Annotation3": "AnnotationValue3",
		"Annotation4": "AnnotationValue4",
		"Annotation5": "AnnotationValue5",
	}
	labels = resource.Labels{
		"Label1": "LabelValue1",
		"Label2": "LabelValue2",
		"Label3": "LabelValue3",
		"Label4": "LabelValue4",
		"Label5": "LabelValue5",
	}
	benchServiceName = "service1"
)

func BenchmarkEndpointNoChange(b *testing.B) {
	b.StopTimer()

	handler := newBenchHandler()

	// Initialize the node and pod caches.
	loadNodesAndPods(handler)

	// Add the service.
	handler.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: newService(),
	})

	// Add the endpoints for all IPs.
	handler.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: newEndpoints(ips...),
	})

	// Create an update event with no changes to the endpoints.
	updateEvent := resource.Event{
		Kind:  resource.Updated,
		Entry: newEndpoints(ips...),
	}

	version := uint64(1)

	b.StartTimer()

	for i := 0; i < b.N; i++ {
		updateEvent.Entry.ID.Version = resource.Version(strconv.FormatUint(version, 10))
		version++
		handler.Handle(updateEvent)
	}
}

func BenchmarkEndpointChurn(b *testing.B) {
	b.StopTimer()

	handler := newBenchHandler()

	// Initialize the node and pod caches.
	loadNodesAndPods(handler)

	// Add the service.
	handler.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: newService(),
	})

	// Add the endpoints for all IPs.
	handler.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: newEndpoints(ips...),
	})

	// Create a sequence of endpoint updates to simulate pod churn.
	updateEntries := []resource.Entry{
		// Slowly take away a few (the even indices).
		newEndpoints(ips[1], ips[2], ips[3], ips[4], ips[5], ips[6], ips[7], ips[8], ips[9]),
		newEndpoints(ips[1], ips[3], ips[4], ips[5], ips[6], ips[7], ips[8], ips[9]),
		newEndpoints(ips[1], ips[3], ips[5], ips[6], ips[7], ips[8], ips[9]),
		newEndpoints(ips[1], ips[3], ips[5], ips[7], ips[8], ips[9]),
		newEndpoints(ips[1], ips[3], ips[5], ips[7], ips[9]),

		// Slowly rebuild the endpoints until we get back to the original list.
		newEndpoints(ips[0], ips[1], ips[3], ips[5], ips[7], ips[9]),
		newEndpoints(ips[0], ips[1], ips[2], ips[3], ips[5], ips[7], ips[9]),
		newEndpoints(ips[0], ips[1], ips[2], ips[3], ips[4], ips[5], ips[7], ips[9]),
		newEndpoints(ips[0], ips[1], ips[2], ips[3], ips[4], ips[5], ips[6], ips[7], ips[9]),
		newEndpoints(ips...),
	}

	// Convert the entries to a list of update events.
	updateEvents := make([]resource.Event, 0, len(updateEntries))
	for _, entry := range updateEntries {
		updateEvents = append(updateEvents, resource.Event{
			Kind:  resource.Updated,
			Entry: entry,
		})
	}

	lenUpdateEvents := len(updateEvents)
	updateIndex := 0
	version := uint64(1)

	b.StartTimer()

	for i := 0; i < b.N; i++ {
		// Get the next update event.
		update := updateEvents[updateIndex]
		updateIndex = (updateIndex + 1) % lenUpdateEvents

		update.Entry.ID.Version = resource.Version(strconv.FormatUint(version, 10))
		version++

		handler.Handle(update)
	}
}

func loadNodesAndPods(handler processing.Handler) {
	saIndex := 0
	for i, ip := range ips {

		// Build the node.
		nodeName := "node" + strconv.Itoa(i)
		handler.Handle(resource.Event{
			Kind: resource.Added,
			Entry: resource.Entry{
				ID: resource.VersionedKey{
					Key: resource.Key{
						FullName:   resource.FullNameFromNamespaceAndName("", nodeName),
						Collection: metadata.K8sCoreV1Nodes.Collection,
					},
					Version: "0",
				},
				Metadata: resource.Metadata{
					CreateTime: createTime,
					Labels: resource.Labels{
						apis.LabelZoneRegion:        region,
						apis.LabelZoneFailureDomain: zone,
					},
				},
				Item: &coreV1.NodeSpec{},
			},
		})

		// Build the pod for this node.
		podName := "pod" + strconv.Itoa(i)
		serviceAccount := serviceAccounts[saIndex]
		saIndex = (saIndex + 1) % len(serviceAccounts)
		handler.Handle(resource.Event{
			Kind: resource.Added,
			Entry: resource.Entry{
				ID: resource.VersionedKey{
					Key: resource.Key{
						FullName:   resource.FullNameFromNamespaceAndName(namespace, podName),
						Collection: metadata.K8sCoreV1Pods.Collection,
					},
					Version: "0",
				},
				Metadata: resource.Metadata{
					CreateTime: createTime,
				},
				Item: &coreV1.Pod{
					ObjectMeta: metaV1.ObjectMeta{
						Name:      podName,
						Namespace: namespace,
					},
					Spec: coreV1.PodSpec{
						NodeName:           nodeName,
						ServiceAccountName: serviceAccount,
					},
					Status: coreV1.PodStatus{
						PodIP: ip,
						Phase: coreV1.PodRunning,
					},
				},
			},
		})
	}
}

func newService() resource.Entry {
	return resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				FullName:   resource.FullNameFromNamespaceAndName(namespace, benchServiceName),
				Collection: metadata.K8sCoreV1Services.Collection,
			},
			Version: resource.Version("0"),
		},
		Metadata: resource.Metadata{
			CreateTime:  createTime,
			Labels:      labels,
			Annotations: annos,
		},
		Item: &coreV1.ServiceSpec{
			Type:      coreV1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.0",
			Ports: []coreV1.ServicePort{
				{
					Name:     "http1",
					Port:     80,
					Protocol: coreV1.ProtocolTCP,
				},
				{
					Name:     "http2",
					Port:     8088,
					Protocol: coreV1.ProtocolTCP,
				},
				{
					Name:     "udp",
					Port:     90,
					Protocol: coreV1.ProtocolUDP,
				},
			},
		},
	}
}

func newEndpoints(ips ...string) resource.Entry {
	addresses := make([]coreV1.EndpointAddress, 0, len(ips))
	for _, ip := range ips {
		addresses = append(addresses, coreV1.EndpointAddress{
			IP: ip,
		})
	}
	return resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				FullName:   resource.FullNameFromNamespaceAndName(namespace, benchServiceName),
				Collection: metadata.K8sCoreV1Endpoints.Collection,
			},
			Version: resource.Version("0"),
		},
		Metadata: resource.Metadata{
			CreateTime:  createTime,
			Labels:      labels,
			Annotations: annos,
		},
		Item: &coreV1.Endpoints{
			ObjectMeta: metaV1.ObjectMeta{
				Name:              benchServiceName,
				Namespace:         namespace,
				CreationTimestamp: metaV1.Time{Time: createTime},
				Labels:            labels,
				Annotations:       annos,
			},
			Subsets: []coreV1.EndpointSubset{
				{
					Addresses: addresses,
					Ports: []coreV1.EndpointPort{
						{
							Name:     "http1",
							Port:     80,
							Protocol: coreV1.ProtocolTCP,
						},
						{
							Name:     "http2",
							Port:     8088,
							Protocol: coreV1.ProtocolTCP,
						},
						{
							Name:     "udp",
							Port:     90,
							Protocol: coreV1.ProtocolUDP,
						},
					},
				},
			},
		},
	}
}

func newBenchHandler() *serviceentry.Handler {
	var handler *serviceentry.Handler
	handler = serviceentry.NewHandler(domain, processing.ListenerFromFn(func(_ resource.Collection) {
		_ = handler.BuildSnapshot()
	}))
	return handler
}
