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
	"testing"
	"time"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	metadata2 "istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/converter"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/pod"
	"istio.io/istio/galley/pkg/runtime/resource"

	coreV1 "k8s.io/api/core/v1"
)

const (
	benchNamespace = "benchmarkns"
)

// BenchmarkService tests the performance of converting a single k8s Service into a networking.ServiceEntry.
func BenchmarkService(b *testing.B) {
	benchmarkService(b, false)
}

// benchmarkService performs work for the Service benchmark. If reuse==true the same output networking.ServiceEntry
// will be used in each iteration. This will enable the benchmark to compare reuse/non-reuse once some form of object
// pooling is supported.
func benchmarkService(b *testing.B, reuse bool) {
	b.Helper()

	b.StopTimer()

	service := &resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				FullName:   resource.FullNameFromNamespaceAndName(benchNamespace, "someservice"),
				Collection: metadata2.K8sCoreV1Services.Collection,
			},
			Version: resource.Version("v1"),
		},
		Metadata: resource.Metadata{
			CreateTime: time.Now(),
			Annotations: resource.Annotations{
				"Annotation1": "AnnotationValue1",
				"Annotation2": "AnnotationValue2",
				"Annotation3": "AnnotationValue3",
				"Annotation4": "AnnotationValue4",
				"Annotation5": "AnnotationValue5",
			},
			Labels: resource.Labels{
				"Label1": "LabelValue1",
				"Label2": "LabelValue2",
				"Label3": "LabelValue3",
				"Label4": "LabelValue4",
				"Label5": "LabelValue5",
			},
		},
		Item: &coreV1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports: []coreV1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					Protocol: coreV1.ProtocolTCP,
				},
				{
					Name:     "https",
					Port:     443,
					Protocol: coreV1.ProtocolTCP,
				},
				{
					Name:     "grpc",
					Port:     8088,
					Protocol: coreV1.ProtocolTCP,
				},
			},
		},
	}

	c := converter.New(domainSuffix, nil)

	// Create/init the output ServiceEntry if reuse is enabled.
	var outMeta *mcp.Metadata
	var out *networking.ServiceEntry
	if reuse {
		outMeta = newMetadata()
		out = newServiceEntry()
		if err := convertService(c, service, outMeta, out); err != nil {
			b.Fatal(err)
		}
	}

	b.StartTimer()

	for i := 0; i < b.N; i++ {
		if err := convertService(c, service, outMeta, out); err != nil {
			b.Fatal(err)
		}
	}
}

func convertService(c *converter.Instance, service *resource.Entry, outMeta *mcp.Metadata, out *networking.ServiceEntry) error {
	if outMeta == nil {
		outMeta = newMetadata()
	}
	if out == nil {
		out = newServiceEntry()
	}
	return c.Convert(service, nil, outMeta, out)
}

// BenchmarkEndpoints tests the performance of converting a single k8s Endpoints resource into a networking.ServiceEntry.
func BenchmarkEndpoints(b *testing.B) {
	benchmarkEndpoints(b, false)
}

// benchmarkEndpoints performs the work for the Endpoints benchmark. If reuse==true the same output networking.ServiceEntry
// will be used in each iteration. This will enable the benchmark to compare reuse/non-reuse once some form of object
// pooling is supported.
func benchmarkEndpoints(b *testing.B, reuse bool) {
	b.Helper()
	b.StopTimer()

	// Establish the list of IPs and service accounts to be used.
	ips := []string{
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
	serviceAccounts := []string{
		"serviceAccount1",
		"serviceAccount2",
		"serviceAccount3",
	}

	// Create the pod/node cache, that will map the IPs to service accounts.
	pods := newPodCache()
	saIndex := 0
	for _, ip := range ips {
		addPod(pods, ip, serviceAccounts[saIndex])
		saIndex = (saIndex + 1) % len(serviceAccounts)
	}

	// Create the k8s Endpoints, splitting the available IPs between the number of subsets.
	numSubsets := 2
	ipsPerSubset := len(ips) / numSubsets
	ipIndex := 0
	endpoints := &coreV1.Endpoints{}
	for subsetIndex := 0; subsetIndex < numSubsets; subsetIndex++ {
		subset := coreV1.EndpointSubset{
			Ports: []coreV1.EndpointPort{
				{
					Name:     "http",
					Port:     80,
					Protocol: coreV1.ProtocolTCP,
				},
				{
					Name:     "https",
					Port:     443,
					Protocol: coreV1.ProtocolTCP,
				},
				{
					Name:     "grpc",
					Port:     8088,
					Protocol: coreV1.ProtocolTCP,
				},
			},
		}
		endIndex := min(ipIndex+ipsPerSubset, len(ips))
		for ; ipIndex < endIndex; ipIndex++ {
			subset.Addresses = append(subset.Addresses, coreV1.EndpointAddress{
				IP: ips[ipIndex],
			})
		}
		endpoints.Subsets = append(endpoints.Subsets, subset)
	}

	entry := &resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				FullName:   resource.FullNameFromNamespaceAndName(benchNamespace, "someservice"),
				Collection: metadata2.K8sCoreV1Endpoints.Collection,
			},
			Version: resource.Version("v1"),
		},
		Metadata: resource.Metadata{
			CreateTime: time.Now(),
			Annotations: resource.Annotations{
				"Annotation1": "AnnotationValue1",
				"Annotation2": "AnnotationValue2",
				"Annotation3": "AnnotationValue3",
				"Annotation4": "AnnotationValue4",
				"Annotation5": "AnnotationValue5",
			},
			Labels: resource.Labels{
				"Label1": "LabelValue1",
				"Label2": "LabelValue2",
				"Label3": "LabelValue3",
				"Label4": "LabelValue4",
				"Label5": "LabelValue5",
			},
		},
		Item: endpoints,
	}

	c := converter.New(domainSuffix, pods)

	// Create/init the output ServiceEntry if reuse is enabled.
	var outMeta *mcp.Metadata
	var out *networking.ServiceEntry
	if reuse {
		outMeta = newMetadata()
		out = newServiceEntry()
		if err := convertEndpoints(c, entry, outMeta, out); err != nil {
			b.Fatal(err)
		}
	}

	b.StartTimer()

	for i := 0; i < b.N; i++ {
		if err := convertEndpoints(c, entry, outMeta, out); err != nil {
			b.Fatal(err)
		}
	}
}

func convertEndpoints(c *converter.Instance, endpoints *resource.Entry, outMeta *mcp.Metadata, out *networking.ServiceEntry) error {
	if outMeta == nil {
		outMeta = newMetadata()
	}
	if out == nil {
		out = newServiceEntry()
	}
	return c.Convert(nil, endpoints, outMeta, out)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func addPod(pods fakePodCache, ip, serviceAccountName string) {
	pods[ip] = pod.Info{
		FullName:           resource.FullNameFromNamespaceAndName(benchNamespace, "SomePod"),
		NodeName:           "SomeNode",
		Locality:           "locality",
		ServiceAccountName: serviceAccountName,
	}
}
