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
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/convert"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis"
)

const (
	domainSuffix = "company.com"
	clusterIP    = "10.0.0.10"
	pod1IP       = "10.0.0.1"
	pod2IP       = "10.0.0.2"
	namespace    = "fakeNamespace"
	nodeName     = "node1"
	region       = "region1"
	zone         = "zone1"
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
	endpointLabels = resource.Labels{
		"eplk1": "eplv1",
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
	h, l := newHandler()

	t.Run("AddNode", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: nodeEntry(nodeName, "v1", region, zone),
		})
		expectNoNotification(t, l)
	})

	t.Run("AddPod1", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: podEntry(name(namespace, "pod1"), "v1", pod1IP, nodeName, "sa1", coreV1.PodRunning),
		})
		expectNoNotification(t, l)
	})

	t.Run("AddPod2", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: podEntry(name(namespace, "pod2"), "v1", pod2IP, nodeName, "sa2", coreV1.PodRunning),
		})
		expectNoNotification(t, l)
	})

	expectedVersion := 0
	t.Run("AddService", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: serviceEntry(serviceName, createTime, "v1"),
		})
		expectNotification(t, l)
		expectedMetadata := newExpectedMetadata(serviceName, createTime, expectedVersion)
		expectedVersion++
		expectedBody := newBuilder().ServiceName(serviceName).Region(region).Zone(zone).Build()
		expectResource(t, h, expectedVersion, expectedMetadata, expectedBody)
	})

	t.Run("UpdateService", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Updated,
			Entry: serviceEntry(serviceName, createTime, "v2"),
		})
		expectNotification(t, l)
		expectedMetadata := newExpectedMetadata(serviceName, createTime, expectedVersion)
		expectedVersion++
		expectedBody := newBuilder().ServiceName(serviceName).Region(region).Zone(zone).Build()
		expectResource(t, h, expectedVersion, expectedMetadata, expectedBody)
	})

	t.Run("AddEndpoints", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: endpointsEntry(serviceName, createTime, "v1", pod1IP),
		})
		expectNotification(t, l)
		expectedMetadata := newExpectedMetadata(serviceName, createTime, expectedVersion)
		expectedVersion++
		expectedBody := newBuilder().ServiceName(serviceName).Region(region).Zone(zone).IPs(pod1IP).
			ServiceAccounts("sa1").Build()
		expectResource(t, h, expectedVersion, expectedMetadata, expectedBody)
	})

	t.Run("ExpandEndpoints", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Updated,
			Entry: endpointsEntry(serviceName, createTime, "v2", pod1IP, pod2IP),
		})
		expectNotification(t, l)
		expectedMetadata := newExpectedMetadata(serviceName, createTime, expectedVersion)
		expectedVersion++
		expectedBody := newBuilder().ServiceName(serviceName).Region(region).Zone(zone).IPs(pod1IP, pod2IP).
			ServiceAccounts("sa1", "sa2").Build()
		expectResource(t, h, expectedVersion, expectedMetadata, expectedBody)
	})

	t.Run("ContractEndpoints", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Updated,
			Entry: endpointsEntry(serviceName, createTime, "v3", pod2IP),
		})
		expectNotification(t, l)
		expectedMetadata := newExpectedMetadata(serviceName, createTime, expectedVersion)
		expectedVersion++
		expectedBody := newBuilder().ServiceName(serviceName).Region(region).Zone(zone).IPs(pod2IP).
			ServiceAccounts("sa2").Build()
		expectResource(t, h, expectedVersion, expectedMetadata, expectedBody)
	})

	t.Run("DeleteEndpoints", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Deleted,
			Entry: endpointsEntry(serviceName, createTime, "v3", pod2IP),
		})
		expectNotification(t, l)
		expectedMetadata := newExpectedMetadata(serviceName, createTime, expectedVersion)
		expectedVersion++
		expectedBody := newBuilder().ServiceName(serviceName).Region(region).Zone(zone).Build()
		expectResource(t, h, expectedVersion, expectedMetadata, expectedBody)
	})

	t.Run("DeleteService", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Deleted,
			Entry: serviceEntry(serviceName, createTime, "v2"),
		})
		expectNotification(t, l)

		expectedVersion++
		expectEmptySnapshot(t, h, expectedVersion)
	})
}

func TestReceiveEndpointsBeforeService(t *testing.T) {
	h, l := newHandler()

	expectedVersion := 0
	t.Run("AddEndpoints", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: endpointsEntry(serviceName, createTime, "v1", pod1IP),
		})
		expectNoNotification(t, l)
		expectEmptySnapshot(t, h, expectedVersion)
	})

	t.Run("AddService", func(t *testing.T) {
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: serviceEntry(serviceName, createTime, "v1"),
		})
		expectNotification(t, l)
		expectedMetadata := newExpectedMetadata(serviceName, createTime, expectedVersion)
		expectedVersion++
		expectedBody := newBuilder().ServiceName(serviceName).IPs(pod1IP).Build()
		expectResource(t, h, expectedVersion, expectedMetadata, expectedBody)
	})
}

func TestAddServiceWithUnknownEventKindShouldNotPanic(t *testing.T) {
	h, l := newHandler()

	h.Handle(resource.Event{
		Kind:  resource.None,
		Entry: serviceEntry(serviceName, createTime, "v1"),
	})
	expectNoNotification(t, l)
}

func TestAddEndpointsWithUnknownEventKindShouldNotPanic(t *testing.T) {
	h, l := newHandler()

	h.Handle(resource.Event{
		Kind:  resource.None,
		Entry: endpointsEntry(serviceName, createTime, "v1"),
	})
	expectNoNotification(t, l)
}

var _ processing.Listener = &fakeListener{}

type fakeListener struct {
	changed []resource.Collection
}

func (l *fakeListener) CollectionChanged(c resource.Collection) {
	l.changed = append(l.changed, c)
}

func (l *fakeListener) clear() {
	l.changed = l.changed[:0]
}

func newHandler() (*serviceentry.Handler, *fakeListener) {
	l := &fakeListener{}
	h := serviceentry.NewHandler(domainSuffix, l)
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

func nodeEntry(nodeName, version, region, zone string) resource.Entry {
	return resource.Entry{
		ID: id(nodeCollection, name("", nodeName), version),
		Metadata: resource.Metadata{
			Labels: localityLabels(region, zone),
		},
		Item: &coreV1.NodeSpec{},
	}
}

func podEntry(podName resource.FullName, version, ip, nodeName, saName string, phase coreV1.PodPhase) resource.Entry {
	ns, name := podName.InterpretAsNamespaceAndName()
	return resource.Entry{
		ID: id(podCollection, podName, version),
		Item: &coreV1.Pod{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: coreV1.PodSpec{
				NodeName:           nodeName,
				ServiceAccountName: saName,
			},
			Status: coreV1.PodStatus{
				PodIP: ip,
				Phase: phase,
			},
		},
	}
}

func serviceEntry(serviceName resource.FullName, createTime time.Time, version string) resource.Entry {
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

func endpointsEntry(serviceName resource.FullName, createTime time.Time, version string, ips ...string) resource.Entry {
	ns, n := serviceName.InterpretAsNamespaceAndName()

	eps := &coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			CreationTimestamp: metaV1.Time{Time: createTime},
			Name:              n,
			Namespace:         ns,
			Labels:            endpointLabels,
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

	for _, ip := range ips {
		eps.Subsets[0].Addresses = append(eps.Subsets[0].Addresses, coreV1.EndpointAddress{
			IP: ip,
		})
	}

	return resource.Entry{
		ID: id(endpointsCollection, serviceName, version),
		Metadata: resource.Metadata{
			CreateTime:  createTime,
			Annotations: serviceAnnotations,
			Labels:      endpointLabels,
		},
		Item: eps,
	}
}

func host(namespace, serviceName string) string {
	return fmt.Sprintf("%s.%s.svc.%s", serviceName, namespace, domainSuffix)
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

func newExpectedMetadata(serviceName resource.FullName, createTime time.Time, version int) *mcp.Metadata {
	expectedCreateTime, _ := types.TimestampProto(createTime)

	return &mcp.Metadata{
		Name:        serviceName.String(),
		Version:     strconv.Itoa(version),
		CreateTime:  expectedCreateTime,
		Labels:      serviceLabels,
		Annotations: convert.Annotations(serviceAnnotations),
	}
}

type builder struct {
	serviceName     resource.FullName
	region          string
	zone            string
	ips             []string
	serviceAccounts []string
}

func newBuilder() *builder {
	return &builder{}
}

func (b *builder) ServiceName(serviceName resource.FullName) *builder {
	b.serviceName = serviceName
	return b
}

func (b *builder) Region(region string) *builder {
	b.region = region
	return b
}

func (b *builder) Zone(zone string) *builder {
	b.zone = zone
	return b
}

func (b *builder) IPs(ips ...string) *builder {
	b.ips = ips
	return b
}

func (b *builder) ServiceAccounts(serviceAccounts ...string) *builder {
	b.serviceAccounts = serviceAccounts
	return b
}

func (b *builder) Build() *networking.ServiceEntry {
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
			Labels:  endpointLabels,
			Address: ip,
			Ports: map[string]uint32{
				"http": 80,
			},

			Locality: localityFor(b.region, b.zone),
		})
	}

	return entry
}

func localityFor(region, zone string) string {
	if region != "" || zone != "" {
		return fmt.Sprintf("%s/%s", region, zone)
	}
	return ""
}

func expectNoNotification(t *testing.T, l *fakeListener) {
	t.Helper()
	if len(l.changed) != 0 {
		t.Fatalf("expected 0 notifications, found %d", len(l.changed))
	}
}

func expectNotification(t *testing.T, l *fakeListener) {
	t.Helper()
	if len(l.changed) != 1 {
		t.Fatalf("expected 1 notification, found %d", len(l.changed))
	}
	if l.changed[0] != serviceEntryCollection {
		t.Fatalf("expected %s to equal %s", l.changed[0], serviceEntryCollection)
	}
	l.clear()
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
		t.Fatalf("expected:\n%+v\nto equal:\n%+v\n", actualMetadata, expectedMetadata)
	}
	if !reflect.DeepEqual(actualBody, expectedBody) {
		t.Fatalf("expected:\n%+v\nto equal:\n%+v\n", actualBody, expectedBody)
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
