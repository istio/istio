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

package converter_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	metadata2 "istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/annotations"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/converter"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/pod"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	domainSuffix = "company.com"
	namespace    = "ns"
	serviceName  = "svc1"
	ip           = "10.0.0.1"
	version      = "v1"
)

var (
	fullName = resource.FullNameFromNamespaceAndName(namespace, serviceName)

	tnow    = time.Now()
	tnowMcp = func() *types.Timestamp {
		t, err := types.TimestampProto(tnow)
		if err != nil {
			panic(err)
		}
		return t
	}()

	serviceID = resource.VersionedKey{
		Key: resource.Key{
			Collection: metadata2.K8sCoreV1Services.Collection,
			FullName:   fullName,
		},
		Version: resource.Version(version),
	}

	endpointsID = resource.VersionedKey{
		Key: resource.Key{
			Collection: metadata2.K8sCoreV1Endpoints.Collection,
			FullName:   fullName,
		},
		Version: resource.Version(version),
	}

	podLabels = map[string]string{
		"pl1": "v1",
		"pl2": "v2",
	}
)

func TestServiceDefaults(t *testing.T) {
	g := NewGomegaWithT(t)

	service := &resource.Entry{
		ID: serviceID,
		Metadata: resource.Metadata{
			CreateTime: tnow,
			Labels: resource.Labels{
				"l1": "v1",
				"l2": "v2",
			},
			Annotations: resource.Annotations{
				"a1": "v1",
				"a2": "v2",
			},
		},
		Item: &coreV1.ServiceSpec{
			ClusterIP: ip,
			Ports: []coreV1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: coreV1.ProtocolTCP,
				},
			},
		},
	}

	expectedMeta := mcp.Metadata{
		Name:       service.ID.FullName.String(),
		CreateTime: tnowMcp,
		Labels: resource.Labels{
			"l1": "v1",
			"l2": "v2",
		},
		Annotations: resource.Annotations{
			"a1":                       "v1",
			"a2":                       "v2",
			annotations.ServiceVersion: version,
		},
	}
	expected := networking.ServiceEntry{
		Hosts:      []string{hostForNamespace(namespace)},
		Addresses:  []string{ip},
		Resolution: networking.ServiceEntry_STATIC,
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Ports: []*networking.Port{
			{
				Name:     "http",
				Number:   8080,
				Protocol: "HTTP",
			},
		},
		Endpoints: []*networking.ServiceEntry_Endpoint{},
	}
	actualMeta, actual := doConvert(t, service, nil, newPodCache())
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func TestServiceExportTo(t *testing.T) {
	g := NewGomegaWithT(t)

	service := &resource.Entry{
		ID: serviceID,
		Metadata: resource.Metadata{
			CreateTime: tnow,
			Annotations: resource.Annotations{
				kube.ServiceExportAnnotation: "c, a, b",
			},
		},
		Item: &coreV1.ServiceSpec{
			ClusterIP: ip,
		},
	}

	expectedMeta := mcp.Metadata{
		Name:       service.ID.FullName.String(),
		CreateTime: tnowMcp,
		Annotations: resource.Annotations{
			kube.ServiceExportAnnotation: "c, a, b",
			annotations.ServiceVersion:   "v1",
		},
	}

	expected := networking.ServiceEntry{
		Hosts:      []string{hostForNamespace(namespace)},
		Addresses:  []string{ip},
		Resolution: networking.ServiceEntry_STATIC,
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Ports:      []*networking.Port{},
		Endpoints:  []*networking.ServiceEntry_Endpoint{},
		ExportTo:   []string{"a", "b", "c"},
	}
	actualMeta, actual := doConvert(t, service, nil, newPodCache())
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func TestNoNamespaceShouldUseDefault(t *testing.T) {
	g := NewGomegaWithT(t)

	ip := "10.0.0.1"
	service := &resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				Collection: metadata2.K8sCoreV1Services.Collection,
				FullName:   resource.FullNameFromNamespaceAndName("", serviceName),
			},
			Version: resource.Version("v1"),
		},
		Metadata: resource.Metadata{
			CreateTime: tnow,
		},
		Item: &coreV1.ServiceSpec{
			ClusterIP: ip,
		},
	}

	expectedMeta := mcp.Metadata{
		Name:       service.ID.FullName.String(),
		CreateTime: tnowMcp,
		Annotations: resource.Annotations{
			annotations.ServiceVersion: "v1",
		},
	}

	expected := networking.ServiceEntry{
		Hosts:      []string{hostForNamespace(coreV1.NamespaceDefault)},
		Addresses:  []string{ip},
		Resolution: networking.ServiceEntry_STATIC,
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Ports:      []*networking.Port{},
		Endpoints:  []*networking.ServiceEntry_Endpoint{},
	}

	actualMeta, actual := doConvert(t, service, nil, newPodCache())
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func TestServicePorts(t *testing.T) {
	cases := []struct {
		name  string
		proto coreV1.Protocol
		out   model.Protocol
	}{
		{"", coreV1.ProtocolTCP, model.ProtocolTCP},
		{"http", coreV1.ProtocolTCP, model.ProtocolHTTP},
		{"http-test", coreV1.ProtocolTCP, model.ProtocolHTTP},
		{"http", coreV1.ProtocolUDP, model.ProtocolUDP},
		{"httptest", coreV1.ProtocolTCP, model.ProtocolTCP},
		{"https", coreV1.ProtocolTCP, model.ProtocolHTTPS},
		{"https-test", coreV1.ProtocolTCP, model.ProtocolHTTPS},
		{"http2", coreV1.ProtocolTCP, model.ProtocolHTTP2},
		{"http2-test", coreV1.ProtocolTCP, model.ProtocolHTTP2},
		{"grpc", coreV1.ProtocolTCP, model.ProtocolGRPC},
		{"grpc-test", coreV1.ProtocolTCP, model.ProtocolGRPC},
		{"grpc-web", coreV1.ProtocolTCP, model.ProtocolGRPCWeb},
		{"grpc-web-test", coreV1.ProtocolTCP, model.ProtocolGRPCWeb},
		{"mongo", coreV1.ProtocolTCP, model.ProtocolMongo},
		{"mongo-test", coreV1.ProtocolTCP, model.ProtocolMongo},
		{"redis", coreV1.ProtocolTCP, model.ProtocolRedis},
		{"redis-test", coreV1.ProtocolTCP, model.ProtocolRedis},
		{"mysql", coreV1.ProtocolTCP, model.ProtocolMySQL},
		{"mysql-test", coreV1.ProtocolTCP, model.ProtocolMySQL},
	}

	ip := "10.0.0.1"
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_[%s]", c.proto, c.name), func(t *testing.T) {
			g := NewGomegaWithT(t)

			service := &resource.Entry{
				ID: serviceID,
				Metadata: resource.Metadata{
					CreateTime: tnow,
				},
				Item: &coreV1.ServiceSpec{
					ClusterIP: ip,
					Ports: []coreV1.ServicePort{
						{
							Name:     c.name,
							Port:     8080,
							Protocol: c.proto,
						},
					},
				},
			}

			expectedMeta := mcp.Metadata{
				Name:       service.ID.FullName.String(),
				CreateTime: tnowMcp,
				Annotations: resource.Annotations{
					annotations.ServiceVersion: version,
				},
			}
			expected := networking.ServiceEntry{
				Hosts:      []string{hostForNamespace(namespace)},
				Addresses:  []string{ip},
				Resolution: networking.ServiceEntry_STATIC,
				Location:   networking.ServiceEntry_MESH_INTERNAL,
				Ports: []*networking.Port{
					{
						Name:     c.name,
						Number:   8080,
						Protocol: string(c.out),
					},
				},
				Endpoints: []*networking.ServiceEntry_Endpoint{},
			}

			actualMeta, actual := doConvert(t, service, nil, newPodCache())
			g.Expect(actualMeta).To(Equal(expectedMeta))
			g.Expect(actual).To(Equal(expected))
		})
	}
}

func TestClusterIPWithNoResolution(t *testing.T) {
	cases := []struct {
		name      string
		clusterIP string
	}{
		{
			name:      "Unspecified",
			clusterIP: "",
		},
		{
			name:      "None",
			clusterIP: coreV1.ClusterIPNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			service := &resource.Entry{
				ID: serviceID,
				Metadata: resource.Metadata{
					CreateTime: tnow,
				},
				Item: &coreV1.ServiceSpec{
					ClusterIP: c.clusterIP,
				},
			}

			expectedMeta := mcp.Metadata{
				Name:       service.ID.FullName.String(),
				CreateTime: tnowMcp,
				Annotations: resource.Annotations{
					annotations.ServiceVersion: version,
				},
			}
			expected := networking.ServiceEntry{
				Hosts:      []string{hostForNamespace(namespace)},
				Addresses:  []string{model.UnspecifiedIP},
				Resolution: networking.ServiceEntry_NONE,
				Location:   networking.ServiceEntry_MESH_INTERNAL,
				Ports:      []*networking.Port{},
				Endpoints:  []*networking.ServiceEntry_Endpoint{},
			}

			actualMeta, actual := doConvert(t, service, nil, newPodCache())
			g.Expect(actualMeta).To(Equal(expectedMeta))
			g.Expect(actual).To(Equal(expected))
		})
	}
}

func TestExternalService(t *testing.T) {
	g := NewGomegaWithT(t)

	externalName := "myexternalsvc"
	service := &resource.Entry{
		ID: serviceID,
		Metadata: resource.Metadata{
			CreateTime: tnow,
		},
		Item: &coreV1.ServiceSpec{
			Type:         coreV1.ServiceTypeExternalName,
			ExternalName: externalName,
			Ports: []coreV1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: coreV1.ProtocolTCP,
				},
			},
		},
	}

	expectedMeta := mcp.Metadata{
		Name:       service.ID.FullName.String(),
		CreateTime: tnowMcp,
		Annotations: resource.Annotations{
			annotations.ServiceVersion: version,
		},
	}
	expected := networking.ServiceEntry{
		Hosts:      []string{hostForNamespace(namespace)},
		Addresses:  []string{model.UnspecifiedIP},
		Resolution: networking.ServiceEntry_DNS,
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Ports: []*networking.Port{
			{
				Name:     "http",
				Number:   8080,
				Protocol: "HTTP",
			},
		},
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: externalName,
				Ports: map[string]uint32{
					"http": 8080,
				},
			},
		},
	}

	actualMeta, actual := doConvert(t, service, nil, newPodCache())
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func TestEndpointsWithNoSubsets(t *testing.T) {
	g := NewGomegaWithT(t)

	endpoints := &resource.Entry{
		ID: endpointsID,
		Metadata: resource.Metadata{
			CreateTime: tnow,
		},
		Item: &coreV1.Endpoints{},
	}

	expectedMeta := mcp.Metadata{
		Annotations: resource.Annotations{
			annotations.EndpointsVersion: version,
		},
	}
	expected := networking.ServiceEntry{
		Endpoints:       []*networking.ServiceEntry_Endpoint{},
		SubjectAltNames: []string{},
	}

	actualMeta, actual := doConvert(t, nil, endpoints, newPodCache())
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func TestEndpoints(t *testing.T) {
	g := NewGomegaWithT(t)

	ip1 := "10.0.0.1"
	ip2 := "10.0.0.2"
	ip3 := "10.0.0.3"
	l1 := "locality1"
	l2 := "locality2"
	cache := fakePodCache{
		ip1: {
			NodeName:           "node1",
			Locality:           l1,
			FullName:           resource.FullNameFromNamespaceAndName(namespace, "pod1"),
			ServiceAccountName: "sa1",
			Labels:             podLabels,
		},
		ip2: {
			NodeName:           "node2",
			Locality:           l2,
			FullName:           resource.FullNameFromNamespaceAndName(namespace, "pod2"),
			ServiceAccountName: "sa2",
			Labels:             podLabels,
		},
		ip3: {
			NodeName:           "node1", // Also on node1
			Locality:           l1,
			FullName:           resource.FullNameFromNamespaceAndName(namespace, "pod3"),
			ServiceAccountName: "sa1", // Same service account as pod1 to test duplicates.
			Labels:             podLabels,
		},
	}

	endpoints := &resource.Entry{
		ID: endpointsID,
		Metadata: resource.Metadata{
			CreateTime: tnow,
		},
		Item: &coreV1.Endpoints{
			ObjectMeta: metaV1.ObjectMeta{},
			Subsets: []coreV1.EndpointSubset{
				{
					NotReadyAddresses: []coreV1.EndpointAddress{
						{
							IP: ip1,
						},
						{
							IP: ip2,
						},
						{
							IP: ip3,
						},
					},
					Addresses: []coreV1.EndpointAddress{
						{
							IP: ip1,
						},
						{
							IP: ip2,
						},
						{
							IP: ip3,
						},
					},
					Ports: []coreV1.EndpointPort{
						{
							Name:     "http",
							Protocol: coreV1.ProtocolTCP,
							Port:     80,
						},
						{
							Name:     "https",
							Protocol: coreV1.ProtocolTCP,
							Port:     443,
						},
					},
				},
			},
		},
	}

	expectedMeta := mcp.Metadata{
		Annotations: resource.Annotations{
			annotations.EndpointsVersion: version,
			annotations.NotReadyEndpoints: fmt.Sprintf("%s:%d,%s:%d,%s:%d,%s:%d,%s:%d,%s:%d",
				ip1, 80,
				ip2, 80,
				ip3, 80,
				ip1, 443,
				ip2, 443,
				ip3, 443),
		},
	}
	expected := networking.ServiceEntry{
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Labels:   podLabels,
				Address:  ip1,
				Locality: l1,
				Ports: map[string]uint32{
					"http":  80,
					"https": 443,
				},
			},
			{
				Labels:   podLabels,
				Address:  ip2,
				Locality: l2,
				Ports: map[string]uint32{
					"http":  80,
					"https": 443,
				},
			},
			{
				Labels:   podLabels,
				Address:  ip3,
				Locality: l1,
				Ports: map[string]uint32{
					"http":  80,
					"https": 443,
				},
			},
		},
		SubjectAltNames: []string{
			"sa1",
			"sa2",
		},
	}

	actualMeta, actual := doConvert(t, nil, endpoints, cache)
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func TestEndpointsPodNotFound(t *testing.T) {
	g := NewGomegaWithT(t)

	endpoints := &resource.Entry{
		ID: endpointsID,
		Metadata: resource.Metadata{
			CreateTime: tnow,
		},
		Item: &coreV1.Endpoints{
			ObjectMeta: metaV1.ObjectMeta{},
			Subsets: []coreV1.EndpointSubset{
				{
					Addresses: []coreV1.EndpointAddress{
						{
							IP: ip,
						},
					},
					Ports: []coreV1.EndpointPort{
						{
							Name:     "http",
							Protocol: coreV1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			},
		},
	}

	expectedMeta := mcp.Metadata{
		Annotations: resource.Annotations{
			annotations.EndpointsVersion: version,
		},
	}
	expected := networking.ServiceEntry{
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address:  ip,
				Locality: "",
				Ports: map[string]uint32{
					"http": 80,
				},
			},
		},
		SubjectAltNames: []string{},
	}

	actualMeta, actual := doConvert(t, nil, endpoints, newPodCache())
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func TestEndpointsNodeNotFound(t *testing.T) {
	g := NewGomegaWithT(t)

	cache := fakePodCache{
		ip: {
			NodeName:           "node1",
			FullName:           resource.FullNameFromNamespaceAndName(namespace, "pod1"),
			ServiceAccountName: "sa1",
			Labels:             podLabels,
		},
	}

	endpoints := &resource.Entry{
		ID: endpointsID,
		Metadata: resource.Metadata{
			CreateTime: tnow,
		},
		Item: &coreV1.Endpoints{
			Subsets: []coreV1.EndpointSubset{
				{
					Addresses: []coreV1.EndpointAddress{
						{
							IP: ip,
						},
					},
					Ports: []coreV1.EndpointPort{
						{
							Name:     "http",
							Protocol: coreV1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			},
		},
	}

	expectedMeta := mcp.Metadata{
		Annotations: resource.Annotations{
			annotations.EndpointsVersion: version,
		},
	}
	expected := networking.ServiceEntry{
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address:  ip,
				Locality: "",
				Ports: map[string]uint32{
					"http": 80,
				},
				Labels: podLabels,
			},
		},
		SubjectAltNames: []string{"sa1"},
	}

	actualMeta, actual := doConvert(t, nil, endpoints, cache)
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func doConvert(t *testing.T, service *resource.Entry, endpoints *resource.Entry, pods pod.Cache) (mcp.Metadata, networking.ServiceEntry) {
	actualMeta := newMetadata()
	actual := newServiceEntry()
	c := newInstance(pods)
	if err := c.Convert(service, endpoints, actualMeta, actual); err != nil {
		t.Fatal(err)
	}
	return *actualMeta, *actual
}

func newInstance(pods pod.Cache) *converter.Instance {
	return converter.New(domainSuffix, pods)
}

func newServiceEntry() *networking.ServiceEntry {
	return &networking.ServiceEntry{}
}

func newMetadata() *mcp.Metadata {
	return &mcp.Metadata{
		Annotations: make(map[string]string),
	}
}

func hostForNamespace(namespace string) string {
	return fmt.Sprintf("%s.%s.svc.%s", serviceName, namespace, domainSuffix)
}

var _ pod.Cache = newPodCache()

type fakePodCache map[string]pod.Info

func newPodCache() fakePodCache {
	return make(fakePodCache)
}

func (c fakePodCache) GetPodByIP(ip string) (pod.Info, bool) {
	p, ok := c[ip]
	return p, ok
}
