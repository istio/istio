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

package convert_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/annotations"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/convert"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/pod"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	domainSuffix = "company.com"
)

var (
	podLabels = map[string]string{
		"l1": "v1",
		"l2": "v2",
	}
)

func TestServiceDefaults(t *testing.T) {
	g := NewGomegaWithT(t)

	serviceName := "service1"
	namespace := "default"
	ip := "10.0.0.1"

	fullName := resource.FullNameFromNamespaceAndName(namespace, serviceName)
	metadata := resource.Metadata{}
	spec := coreV1.ServiceSpec{
		ClusterIP: ip,
		Ports: []coreV1.ServicePort{
			{
				Name:     "http",
				Port:     8080,
				Protocol: coreV1.ProtocolTCP,
			},
		},
	}

	expected := networking.ServiceEntry{
		Hosts:      []string{host(namespace, serviceName)},
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
	actual := &networking.ServiceEntry{}
	convert.Service(&spec, metadata, fullName, domainSuffix, actual)
	g.Expect(actual).ToNot(BeNil())
	g.Expect(*actual).To(Equal(expected))
}

func TestServiceExportTo(t *testing.T) {
	g := NewGomegaWithT(t)

	fullName := resource.FullNameFromNamespaceAndName("ns", "svc1")
	metadata := resource.Metadata{
		Annotations: resource.Annotations{
			kube.ServiceExportAnnotation: "c, a, b",
		},
	}
	spec := coreV1.ServiceSpec{
		ClusterIP: "10.0.0.1",
	}

	expected := []string{"a", "b", "c"}
	actual := &networking.ServiceEntry{}
	convert.Service(&spec, metadata, fullName, domainSuffix, actual)

	g.Expect(actual).ToNot(BeNil())
	g.Expect(actual.ExportTo).To(Equal(expected))
}

func TestNoNamespaceShouldUseDefault(t *testing.T) {
	g := NewGomegaWithT(t)

	fullName := resource.FullNameFromNamespaceAndName("", "svc1")
	metadata := resource.Metadata{}
	spec := coreV1.ServiceSpec{
		ClusterIP: "10.0.0.1",
	}

	expected := host(coreV1.NamespaceDefault, "svc1")
	actual := &networking.ServiceEntry{}
	convert.Service(&spec, metadata, fullName, domainSuffix, actual)
	g.Expect(actual).ToNot(BeNil())
	g.Expect(len(actual.Hosts)).To(Equal(1))
	g.Expect(actual.Hosts[0]).To(Equal(expected))
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

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_[%s]", c.proto, c.name), func(t *testing.T) {
			g := NewGomegaWithT(t)

			fullName := resource.FullNameFromNamespaceAndName("ns", "svc1")
			metadata := resource.Metadata{}
			spec := coreV1.ServiceSpec{
				ClusterIP: "10.0.0.1",
				Ports: []coreV1.ServicePort{
					{
						Name:     c.name,
						Port:     8080,
						Protocol: c.proto,
					},
				},
			}

			expected := []*networking.Port{
				{
					Name:     c.name,
					Number:   8080,
					Protocol: string(c.out),
				},
			}
			actual := &networking.ServiceEntry{}
			convert.Service(&spec, metadata, fullName, domainSuffix, actual)
			g.Expect(actual).ToNot(BeNil())
			g.Expect(actual.GetPorts()).To(Equal(expected))
		})
	}
}

func TestServiceWithEmptyIPShouldHaveNoResolution(t *testing.T) {
	g := NewGomegaWithT(t)

	fullName := resource.FullNameFromNamespaceAndName("ns", "svc1")
	metadata := resource.Metadata{}
	spec := coreV1.ServiceSpec{
		ClusterIP: "",
	}

	expected := networking.ServiceEntry_NONE
	actual := &networking.ServiceEntry{}
	convert.Service(&spec, metadata, fullName, domainSuffix, actual)
	g.Expect(actual).ToNot(BeNil())
	g.Expect(actual.Resolution).To(Equal(expected))
}

func TestServiceWithIPNoneShouldHaveNoResolution(t *testing.T) {
	g := NewGomegaWithT(t)

	fullName := resource.FullNameFromNamespaceAndName("ns", "svc1")
	metadata := resource.Metadata{}
	spec := coreV1.ServiceSpec{
		ClusterIP: coreV1.ClusterIPNone,
	}

	expected := networking.ServiceEntry_NONE
	actual := &networking.ServiceEntry{}
	convert.Service(&spec, metadata, fullName, domainSuffix, actual)
	g.Expect(actual).ToNot(BeNil())
	g.Expect(actual.Resolution).To(Equal(expected))
}

func TestExternalService(t *testing.T) {
	g := NewGomegaWithT(t)

	namespace := "ns"
	serviceName := "svc1"
	externalName := "myexternalsvc"

	fullName := resource.FullNameFromNamespaceAndName(namespace, serviceName)
	metadata := resource.Metadata{}
	spec := coreV1.ServiceSpec{
		Type:         coreV1.ServiceTypeExternalName,
		ExternalName: externalName,
		Ports: []coreV1.ServicePort{
			{
				Name:     "http",
				Port:     8080,
				Protocol: coreV1.ProtocolTCP,
			},
		},
	}

	expected := networking.ServiceEntry{
		Hosts:      []string{host(namespace, serviceName)},
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
	actual := &networking.ServiceEntry{}
	convert.Service(&spec, metadata, fullName, domainSuffix, actual)
	g.Expect(actual).ToNot(BeNil())
	g.Expect(*actual).To(Equal(expected))
}

func TestConvertAnnotations(t *testing.T) {
	g := NewGomegaWithT(t)

	service := resource.Entry{
		ID: resource.VersionedKey{
			Version: "serviceVersion",
		},
		Metadata: resource.Metadata{
			Annotations: resource.Annotations{
				"a1": "v1",
				"a2": "v2",
			},
		},
	}
	endpoints := resource.Entry{
		ID: resource.VersionedKey{
			Version: "endpointsVersion",
		},
	}

	actual := convert.Annotations(service, &endpoints)
	expected := resource.Annotations{
		"a1":                         "v1",
		"a2":                         "v2",
		annotations.ServiceVersion:   "serviceVersion",
		annotations.EndpointsVersion: "endpointsVersion",
	}
	g.Expect(actual).To(Equal(expected))
}

func TestConvertAnnotationsWithNoEndpoints(t *testing.T) {
	g := NewGomegaWithT(t)

	service := resource.Entry{
		ID: resource.VersionedKey{
			Version: "serviceVersion",
		},
		Metadata: resource.Metadata{
			Annotations: resource.Annotations{
				"a1": "v1",
				"a2": "v2",
			},
		},
	}

	actual := convert.Annotations(service, nil)
	expected := resource.Annotations{
		"a1":                       "v1",
		"a2":                       "v2",
		annotations.ServiceVersion: "serviceVersion",
	}
	g.Expect(actual).To(Equal(expected))
}

func TestEndpointsWithNoSubsets(t *testing.T) {
	g := NewGomegaWithT(t)
	eps := coreV1.Endpoints{}
	cache := &fakeCache{}

	expectedEndpoints := make([]*networking.ServiceEntry_Endpoint, 0)
	expectedSubjectAltNames := make([]string, 0)
	actual := &networking.ServiceEntry{}
	convert.Endpoints(&eps, cache, actual)
	g.Expect(actual.Endpoints).To(Equal(expectedEndpoints))
	g.Expect(actual.SubjectAltNames).To(Equal(expectedSubjectAltNames))
}

func TestEndpoints(t *testing.T) {
	g := NewGomegaWithT(t)
	ip1 := "10.0.0.1"
	ip2 := "10.0.0.2"
	ip3 := "10.0.0.3"
	l1 := "locality1"
	l2 := "locality2"
	cache := &fakeCache{
		pods: map[string]pod.Info{
			ip1: {
				NodeName:           "node1",
				Locality:           l1,
				FullName:           resource.FullNameFromNamespaceAndName("ns", "pod1"),
				ServiceAccountName: "sa1",
				Labels:             podLabels,
			},
			ip2: {
				NodeName:           "node2",
				Locality:           l2,
				FullName:           resource.FullNameFromNamespaceAndName("ns", "pod2"),
				ServiceAccountName: "sa2",
				Labels:             podLabels,
			},
			ip3: {
				NodeName:           "node1", // Also on node1
				Locality:           l1,
				FullName:           resource.FullNameFromNamespaceAndName("ns", "pod3"),
				ServiceAccountName: "sa1", // Same service account as pod1 to test duplicates.
				Labels:             podLabels,
			},
		},
	}
	labels := map[string]string{
		"l1": "v1",
		"l2": "v2",
	}
	eps := coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			Labels: labels,
		},
		Subsets: []coreV1.EndpointSubset{
			{
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
	}

	expectedEndpoints := []*networking.ServiceEntry_Endpoint{
		{
			Labels:   labels,
			Address:  ip1,
			Locality: l1,
			Ports: map[string]uint32{
				"http":  80,
				"https": 443,
			},
		},
		{
			Labels:   labels,
			Address:  ip2,
			Locality: l2,
			Ports: map[string]uint32{
				"http":  80,
				"https": 443,
			},
		},
		{
			Labels:   labels,
			Address:  ip3,
			Locality: l1,
			Ports: map[string]uint32{
				"http":  80,
				"https": 443,
			},
		},
	}

	// Expect that we'll remove duplicate service accounts.
	expectedSubjectAltNames := []string{
		"sa1",
		"sa2",
	}
	actual := &networking.ServiceEntry{}
	convert.Endpoints(&eps, cache, actual)

	g.Expect(actual.Endpoints).To(Equal(expectedEndpoints))
	g.Expect(actual.SubjectAltNames).To(Equal(expectedSubjectAltNames))
}

func TestEndpointsPodNotFound(t *testing.T) {
	g := NewGomegaWithT(t)
	ip1 := "10.0.0.1"
	cache := &fakeCache{}
	eps := coreV1.Endpoints{
		Subsets: []coreV1.EndpointSubset{
			{
				Addresses: []coreV1.EndpointAddress{
					{
						IP: ip1,
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
	}

	expectedEndpoints := []*networking.ServiceEntry_Endpoint{
		{
			Address:  ip1,
			Locality: "",
			Ports: map[string]uint32{
				"http": 80,
			},
		},
	}
	expectedSubjectAltNames := make([]string, 0)
	actual := &networking.ServiceEntry{}
	convert.Endpoints(&eps, cache, actual)
	g.Expect(actual.Endpoints).To(Equal(expectedEndpoints))
	g.Expect(actual.SubjectAltNames).To(Equal(expectedSubjectAltNames))
}

func TestEndpointsNodeNotFound(t *testing.T) {
	g := NewGomegaWithT(t)
	ip1 := "10.0.0.1"
	cache := &fakeCache{
		pods: map[string]pod.Info{
			ip1: {
				NodeName:           "node1",
				FullName:           resource.FullNameFromNamespaceAndName("ns", "pod1"),
				ServiceAccountName: "sa1",
				Labels:             podLabels,
			},
		},
	}
	eps := coreV1.Endpoints{
		Subsets: []coreV1.EndpointSubset{
			{
				Addresses: []coreV1.EndpointAddress{
					{
						IP: ip1,
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
	}

	expectedEndpoints := []*networking.ServiceEntry_Endpoint{
		{
			Address:  ip1,
			Locality: "",
			Ports: map[string]uint32{
				"http": 80,
			},
			Labels: podLabels,
		},
	}
	expectedSubjectAltNames := []string{"sa1"}
	actual := &networking.ServiceEntry{}
	convert.Endpoints(&eps, cache, actual)
	g.Expect(actual.Endpoints).To(Equal(expectedEndpoints))
	g.Expect(actual.SubjectAltNames).To(Equal(expectedSubjectAltNames))
}

func host(namespace, serviceName string) string {
	return fmt.Sprintf("%s.%s.svc.%s", serviceName, namespace, domainSuffix)
}

var _ pod.Cache = &fakeCache{}

type fakeCache struct {
	pods map[string]pod.Info
}

func (c *fakeCache) GetPodByIP(ip string) (pod.Info, bool) {
	p, ok := c.pods[ip]
	return p, ok
}
