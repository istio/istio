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

	. "github.com/onsi/gomega"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/processor/transforms/serviceentry/converter"
	"istio.io/istio/galley/pkg/config/processor/transforms/serviceentry/pod"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/validation"
)

const (
	domainSuffix = "company.com"
	namespace    = "ns"
	serviceName  = "svc1"
	ip           = "10.0.0.1"
	version      = "v1"
)

var (
	fullName = resource.NewFullName(namespace, serviceName)

	tnow = time.Now()

	podLabels = map[string]string{
		"pl1": "v1",
		"pl2": "v2",
	}
)

func TestServiceDefaults(t *testing.T) {
	g := NewGomegaWithT(t)

	service := &resource.Instance{
		Metadata: resource.Metadata{
			FullName: fullName,
			Version:  version,

			CreateTime: tnow,
			Labels: resource.StringMap{
				"l1": "v1",
				"l2": "v2",
			},
			Annotations: resource.StringMap{
				"a1": "v1",
				"a2": "v2",
			},
		},
		Message: &coreV1.ServiceSpec{
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

	expectedMeta := resource.Metadata{
		FullName:   service.Metadata.FullName,
		CreateTime: tnow,
		Labels: resource.StringMap{
			"l1": "v1",
			"l2": "v2",
		},
		Annotations: resource.StringMap{
			"a1": "v1",
			"a2": "v2",
			annotation.AlphaNetworkingServiceVersion.Name: version,
		},
	}
	expected := networking.ServiceEntry{
		Hosts:      []string{hostForNamespace(namespace)},
		Addresses:  []string{ip},
		Resolution: networking.ServiceEntry_NONE,
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

func TestServiceResolution(t *testing.T) {
	g := NewGomegaWithT(t)

	externalName := "myexternalsvc"
	tests := []struct {
		name       string
		endpoints  *resource.Instance
		service    *resource.Instance
		resolution networking.ServiceEntry_Resolution
	}{
		{
			name: "DNS resolution",
			service: &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					CreateTime: tnow,
				},
				Message: &coreV1.ServiceSpec{
					Type:         coreV1.ServiceTypeExternalName,
					ExternalName: externalName,
				},
			},
			resolution: networking.ServiceEntry_DNS,
		},
		{
			name: "NONE resolution",
			service: &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					CreateTime: tnow,
				},
				Message: &coreV1.ServiceSpec{
					ClusterIP: constants.UnspecifiedIP,
				},
			},
			resolution: networking.ServiceEntry_NONE,
		},
		{
			name: "STATIC resolution",
			endpoints: &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					CreateTime: tnow,
				},
				Message: &coreV1.Endpoints{
					ObjectMeta: metaV1.ObjectMeta{},
					Subsets: []coreV1.EndpointSubset{
						{
							Addresses: []coreV1.EndpointAddress{
								{
									IP: "10.0.0.1",
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
			},
			service: &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					CreateTime: tnow,
				},
				Message: &coreV1.ServiceSpec{
					ClusterIP: constants.UnspecifiedIP,
				},
			},
			resolution: networking.ServiceEntry_STATIC,
		},
		{
			name: "STATIC resolution",
			endpoints: &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					CreateTime: tnow,
				},
				Message: &coreV1.Endpoints{
					ObjectMeta: metaV1.ObjectMeta{},
					Subsets: []coreV1.EndpointSubset{
						{
							Addresses: []coreV1.EndpointAddress{
								{
									IP: "10.0.0.1",
								},
								{
									IP: "10.0.0.2",
								},
								{
									IP: "10.0.0.3",
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
			},
			service: &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					CreateTime: tnow,
				},
				Message: &coreV1.ServiceSpec{
					Type: coreV1.ServiceTypeNodePort,
					Ports: []coreV1.ServicePort{
						{
							Name:     "http",
							Port:     8080,
							Protocol: coreV1.ProtocolTCP,
						},
					},
				},
			},
			resolution: networking.ServiceEntry_STATIC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, actual := doConvert(t, tt.service, tt.endpoints, newPodCache())
			g.Expect(actual.Resolution).To(Equal(tt.resolution))
			switch tt.resolution {
			case networking.ServiceEntry_DNS:
				g.Expect(len(actual.Addresses)).To(Equal(1))
				g.Expect(actual.Addresses[0]).To(Equal(constants.UnspecifiedIP))
				for _, host := range actual.Hosts {
					g.Expect(validation.ValidateFQDN(host)).To(BeNil())

				}
			case networking.ServiceEntry_STATIC:
				g.Expect(len(actual.Endpoints)).ToNot(BeZero())
			}
		})
	}
}

func TestServiceExportTo(t *testing.T) {
	g := NewGomegaWithT(t)

	service := &resource.Instance{
		Metadata: resource.Metadata{
			FullName:   fullName,
			Version:    resource.Version("v1"),
			CreateTime: tnow,
			Annotations: resource.StringMap{
				annotation.NetworkingExportTo.Name: "c, a, b",
			},
		},
		Message: &coreV1.ServiceSpec{
			ClusterIP: ip,
		},
	}

	expectedMeta := resource.Metadata{
		FullName:   fullName,
		CreateTime: tnow,
		Annotations: resource.StringMap{
			annotation.NetworkingExportTo.Name:            "c, a, b",
			annotation.AlphaNetworkingServiceVersion.Name: "v1",
		},
	}

	expected := networking.ServiceEntry{
		Hosts:      []string{hostForNamespace(namespace)},
		Addresses:  []string{ip},
		Resolution: networking.ServiceEntry_NONE,
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
	service := &resource.Instance{
		Metadata: resource.Metadata{
			FullName:   resource.NewFullName("", serviceName),
			Version:    resource.Version("v1"),
			CreateTime: tnow,
		},
		Message: &coreV1.ServiceSpec{
			ClusterIP: ip,
		},
	}

	expectedMeta := resource.Metadata{
		FullName:   service.Metadata.FullName,
		CreateTime: tnow,
		Annotations: resource.StringMap{
			annotation.AlphaNetworkingServiceVersion.Name: "v1",
		},
	}

	expected := networking.ServiceEntry{
		Hosts:      []string{hostForNamespace(coreV1.NamespaceDefault)},
		Addresses:  []string{ip},
		Resolution: networking.ServiceEntry_NONE,
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Ports:      []*networking.Port{},
		Endpoints:  []*networking.ServiceEntry_Endpoint{},
	}

	actualMeta, actual := doConvert(t, service, nil, newPodCache())
	g.Expect(actualMeta).To(Equal(expectedMeta))
	g.Expect(actual).To(Equal(expected))
}

func TestServicePortValidation(t *testing.T) {
	cases := []struct {
		description   string
		name          string
		number        int32
		proto         coreV1.Protocol
		expectedPorts []*networking.Port
	}{
		{
			description: "valid",
			name:        "http",
			number:      8080,
			proto:       coreV1.ProtocolTCP,
			expectedPorts: []*networking.Port{
				{
					Name:     "http",
					Number:   uint32(8080),
					Protocol: string(protocol.HTTP),
				},
			},
		},
		{
			description:   "empty port name",
			name:          "",
			number:        8080,
			proto:         coreV1.ProtocolTCP,
			expectedPorts: []*networking.Port{},
		},
		{
			description:   "invalid protocol",
			name:          "http-test",
			number:        8080,
			proto:         coreV1.Protocol("XYZ"),
			expectedPorts: []*networking.Port{},
		},
		{
			description:   "unsupported Protocol",
			name:          "http-tcp",
			number:        8080,
			proto:         coreV1.Protocol(protocol.Unsupported),
			expectedPorts: []*networking.Port{},
		},
		{
			description:   "invalid port number",
			name:          "http-tcp",
			number:        1111111,
			proto:         coreV1.ProtocolTCP,
			expectedPorts: []*networking.Port{},
		},
	}
	ip := "10.0.0.1"
	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			g := NewGomegaWithT(t)

			service := &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					Version:    resource.Version("v1"),
					CreateTime: tnow,
				},
				Message: &coreV1.ServiceSpec{
					ClusterIP: ip,
					Ports: []coreV1.ServicePort{
						{
							Name:     c.name,
							Port:     c.number,
							Protocol: c.proto,
						},
					},
				},
			}

			expectedMeta := resource.Metadata{
				FullName:   service.Metadata.FullName,
				CreateTime: tnow,
				Annotations: resource.StringMap{
					annotation.AlphaNetworkingServiceVersion.Name: version,
				},
			}

			expected := networking.ServiceEntry{
				Hosts:      []string{hostForNamespace(namespace)},
				Addresses:  []string{ip},
				Resolution: networking.ServiceEntry_NONE,
				Location:   networking.ServiceEntry_MESH_INTERNAL,
				Ports:      c.expectedPorts,
				Endpoints:  []*networking.ServiceEntry_Endpoint{},
			}

			actualMeta, actual := doConvert(t, service, nil, newPodCache())
			g.Expect(actualMeta).To(Equal(expectedMeta))
			g.Expect(actual).To(Equal(expected))
		})
	}
}

func TestServicePorts(t *testing.T) {
	cases := []struct {
		name  string
		proto coreV1.Protocol
		out   protocol.Instance
	}{
		{"", coreV1.ProtocolTCP, protocol.Unsupported},
		{"http", coreV1.ProtocolTCP, protocol.HTTP},
		{"http-test", coreV1.ProtocolTCP, protocol.HTTP},
		{"http", coreV1.ProtocolUDP, protocol.UDP},
		{"httptest", coreV1.ProtocolTCP, protocol.Unsupported},
		{"https", coreV1.ProtocolTCP, protocol.HTTPS},
		{"https-test", coreV1.ProtocolTCP, protocol.HTTPS},
		{"http2", coreV1.ProtocolTCP, protocol.HTTP2},
		{"http2-test", coreV1.ProtocolTCP, protocol.HTTP2},
		{"grpc", coreV1.ProtocolTCP, protocol.GRPC},
		{"grpc-test", coreV1.ProtocolTCP, protocol.GRPC},
		{"grpc-web", coreV1.ProtocolTCP, protocol.GRPCWeb},
		{"grpc-web-test", coreV1.ProtocolTCP, protocol.GRPCWeb},
		{"mongo", coreV1.ProtocolTCP, protocol.Mongo},
		{"mongo-test", coreV1.ProtocolTCP, protocol.Mongo},
		{"redis", coreV1.ProtocolTCP, protocol.Redis},
		{"redis-test", coreV1.ProtocolTCP, protocol.Redis},
		{"mysql", coreV1.ProtocolTCP, protocol.MySQL},
		{"mysql-test", coreV1.ProtocolTCP, protocol.MySQL},
	}

	ip := "10.0.0.1"
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_[%s]", c.proto, c.name), func(t *testing.T) {
			g := NewGomegaWithT(t)

			service := &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					Version:    resource.Version("v1"),
					CreateTime: tnow,
				},
				Message: &coreV1.ServiceSpec{
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

			expectedMeta := resource.Metadata{
				FullName:   service.Metadata.FullName,
				CreateTime: tnow,
				Annotations: resource.StringMap{
					annotation.AlphaNetworkingServiceVersion.Name: version,
				},
			}

			ports := []*networking.Port{
				{
					Name:     c.name,
					Number:   8080,
					Protocol: string(c.out),
				},
			}
			if c.name == "" {
				ports = []*networking.Port{}
			}
			expected := networking.ServiceEntry{
				Hosts:      []string{hostForNamespace(namespace)},
				Addresses:  []string{ip},
				Resolution: networking.ServiceEntry_NONE,
				Location:   networking.ServiceEntry_MESH_INTERNAL,
				Ports:      ports,
				Endpoints:  []*networking.ServiceEntry_Endpoint{},
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

			service := &resource.Instance{
				Metadata: resource.Metadata{
					FullName:   fullName,
					Version:    resource.Version("v1"),
					CreateTime: tnow,
				},
				Message: &coreV1.ServiceSpec{
					ClusterIP: c.clusterIP,
				},
			}

			expectedMeta := resource.Metadata{
				FullName:   service.Metadata.FullName,
				CreateTime: tnow,
				Annotations: resource.StringMap{
					annotation.AlphaNetworkingServiceVersion.Name: version,
				},
			}
			expected := networking.ServiceEntry{
				Hosts:      []string{hostForNamespace(namespace)},
				Addresses:  []string{constants.UnspecifiedIP},
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
	service := &resource.Instance{
		Metadata: resource.Metadata{
			FullName:   fullName,
			Version:    resource.Version("v1"),
			CreateTime: tnow,
		},
		Message: &coreV1.ServiceSpec{
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

	expectedMeta := resource.Metadata{
		FullName:   service.Metadata.FullName,
		CreateTime: tnow,
		Annotations: resource.StringMap{
			annotation.AlphaNetworkingServiceVersion.Name: version,
		},
	}
	expected := networking.ServiceEntry{
		Hosts:      []string{hostForNamespace(namespace)},
		Addresses:  []string{constants.UnspecifiedIP},
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

	endpoints := &resource.Instance{
		Metadata: resource.Metadata{
			FullName:   fullName,
			Version:    resource.Version("v1"),
			CreateTime: tnow,
		},
		Message: &coreV1.Endpoints{},
	}

	expectedMeta := resource.Metadata{
		Annotations: resource.StringMap{
			annotation.AlphaNetworkingEndpointsVersion.Name: version,
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
			FullName:           resource.NewFullName(namespace, "pod1"),
			ServiceAccountName: "sa1",
			Labels:             podLabels,
		},
		ip2: {
			NodeName:           "node2",
			Locality:           l2,
			FullName:           resource.NewFullName(namespace, "pod2"),
			ServiceAccountName: "sa2",
			Labels:             podLabels,
		},
		ip3: {
			NodeName:           "node1", // Also on node1
			Locality:           l1,
			FullName:           resource.NewFullName(namespace, "pod3"),
			ServiceAccountName: "sa1", // Same service account as pod1 to test duplicates.
			Labels:             podLabels,
		},
	}

	endpoints := &resource.Instance{
		Metadata: resource.Metadata{
			FullName:   fullName,
			Version:    resource.Version("v1"),
			CreateTime: tnow,
		},
		Message: &coreV1.Endpoints{
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

	expectedMeta := resource.Metadata{
		Annotations: resource.StringMap{
			annotation.AlphaNetworkingEndpointsVersion.Name: version,
			annotation.AlphaNetworkingNotReadyEndpoints.Name: fmt.Sprintf("%s:%d,%s:%d,%s:%d,%s:%d,%s:%d,%s:%d",
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

	endpoints := &resource.Instance{
		Metadata: resource.Metadata{
			FullName:   fullName,
			Version:    resource.Version("v1"),
			CreateTime: tnow,
		},
		Message: &coreV1.Endpoints{
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

	expectedMeta := resource.Metadata{
		Annotations: resource.StringMap{
			annotation.AlphaNetworkingEndpointsVersion.Name: version,
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
			FullName:           resource.NewFullName(namespace, "pod1"),
			ServiceAccountName: "sa1",
			Labels:             podLabels,
		},
	}

	endpoints := &resource.Instance{
		Metadata: resource.Metadata{
			FullName:   fullName,
			Version:    resource.Version("v1"),
			CreateTime: tnow,
		},
		Message: &coreV1.Endpoints{
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

	expectedMeta := resource.Metadata{
		Annotations: resource.StringMap{
			annotation.AlphaNetworkingEndpointsVersion.Name: version,
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

func doConvert(t *testing.T, service *resource.Instance, endpoints *resource.Instance, pods pod.Cache) (resource.Metadata, networking.ServiceEntry) {
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

func newMetadata() *resource.Metadata {
	return &resource.Metadata{
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
