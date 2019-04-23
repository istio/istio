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

package integration

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube"
	"istio.io/istio/galley/pkg/source/kube/builtin"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/testing/mock"
	"istio.io/istio/pkg/mcp/snapshot"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/kubelet/apis"
)

const (
	domainSuffix = "company.com"
	namespace    = "fakeNamespace"
	region       = "region1"
	zone         = "zone1"
)

var (
	createTime = time.Now()
	mesh       = meshconfig.NewInMemory()
	runtimeCfg = runtime.Config{
		Mesh:         mesh,
		DomainSuffix: domainSuffix,
	}
	converterCfg = converter.Config{
		Mesh:         mesh,
		DomainSuffix: domainSuffix,
	}

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

// BenchmarkEndpointChurn is an integration-level benchmark for the entire runtime pipeline. This tests the performance
// of the ServiceEntry synthesis for a single service undergoing constant endpoint churn (i.e. endpoints come up and
// down constantly).
func BenchmarkEndpointChurn(b *testing.B) {
	b.StopTimer()

	ki := mock.NewKube()
	kubeClient := newKubeClient(b, ki)

	// Create all of the k8s resources.
	loadNodesAndPods(b, kubeClient)
	loadService(b, kubeClient)
	loadEndpoints(b, kubeClient)

	// Create a sequence of endpoint updates to simulate pod churn.
	updateEntries := []coreV1.Endpoints{
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

	src := newSource(b, ki, builtin.GetSchema().All()...)
	distributor := newFakeDistributor(b.N)
	processor := runtime.NewProcessor(src, distributor, &runtimeCfg)
	if err := processor.Start(); err != nil {
		b.Fatal(err)
	}

	if err := processor.AwaitFullSync(5 * time.Second); err != nil {
		b.Fatal(err)
	}

	lenUpdateEvents := len(updateEntries)
	updateIndex := 0
	version := uint64(1)

	endpoints := make([]*coreV1.Endpoints, b.N)
	for i := 0; i < b.N; i++ {
		update := updateEntries[updateIndex]
		updateIndex = (updateIndex + 1) % lenUpdateEvents

		update.ResourceVersion = strconv.FormatUint(version, 10)
		version++

		endpoints[i] = &update
	}

	b.StartTimer()

	for _, eps := range endpoints {
		if _, err := kubeClient.CoreV1().Endpoints(namespace).Update(eps); err != nil {
			b.Fatal(err)
		}
	}

	distributor.await()

	b.StopTimer()
	processor.Stop()
	b.StartTimer()
}

func loadNodesAndPods(b *testing.B, kubeClient kubernetes.Interface) {
	b.Helper()
	saIndex := 0
	for i, ip := range ips {

		// Build the node.
		nodeName := "node" + strconv.Itoa(i)
		if _, err := kubeClient.CoreV1().Nodes().Create(&coreV1.Node{
			ObjectMeta: metaV1.ObjectMeta{
				Name:            nodeName,
				ResourceVersion: "0",
				CreationTimestamp: metaV1.Time{
					Time: createTime,
				},
				Labels: map[string]string{
					apis.LabelZoneRegion:        region,
					apis.LabelZoneFailureDomain: zone,
				},
			},
			Spec: coreV1.NodeSpec{
				PodCIDR: "10.40.0.0/24",
			},
		}); err != nil {
			b.Fatal(err)
		}

		// Build the pod for this node.
		podName := "pod" + strconv.Itoa(i)
		serviceAccount := serviceAccounts[saIndex]
		saIndex = (saIndex + 1) % len(serviceAccounts)
		if _, err := kubeClient.CoreV1().Pods(namespace).Create(&coreV1.Pod{
			ObjectMeta: metaV1.ObjectMeta{
				Name:            podName,
				Namespace:       namespace,
				ResourceVersion: "0",
				CreationTimestamp: metaV1.Time{
					Time: createTime,
				},
				Labels: map[string]string{
					apis.LabelZoneRegion:        region,
					apis.LabelZoneFailureDomain: zone,
				},
			},
			Spec: coreV1.PodSpec{
				NodeName:           nodeName,
				ServiceAccountName: serviceAccount,
			},
			Status: coreV1.PodStatus{
				PodIP: ip,
				Phase: coreV1.PodRunning,
			},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func loadService(b *testing.B, kubeClient kubernetes.Interface) {
	b.Helper()
	if _, err := kubeClient.CoreV1().Services(namespace).Create(&coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:            benchServiceName,
			Namespace:       namespace,
			ResourceVersion: "0",
			CreationTimestamp: metaV1.Time{
				Time: createTime,
			},
			Labels:      labels,
			Annotations: annos,
		},
		Spec: coreV1.ServiceSpec{
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
	}); err != nil {
		b.Fatal(err)
	}
}

func loadEndpoints(b *testing.B, kubeClient kubernetes.Interface) {
	b.Helper()
	endpoints := newEndpoints(ips...)
	if _, err := kubeClient.CoreV1().Endpoints(namespace).Create(&endpoints); err != nil {
		b.Fatal(err)
	}
}

type fakeDistributor struct {
	cond              *sync.Cond
	serviceCreation   int
	endpointsCreation int
	complete          int
	counter           int
}

func newFakeDistributor(numUpdates int) *fakeDistributor {
	return &fakeDistributor{
		serviceCreation:   1,
		endpointsCreation: 2,
		complete:          2 + numUpdates,
		cond:              sync.NewCond(&sync.Mutex{}),
	}
}

func (d *fakeDistributor) SetSnapshot(name string, s snapshot.Snapshot) {
	d.counter++
	if d.counter == d.serviceCreation || d.counter == d.endpointsCreation || d.counter == d.complete {
		d.cond.L.Lock()
		d.cond.Signal()
		d.cond.L.Unlock()
	}
}

func (d *fakeDistributor) await() {
	d.cond.L.Lock()
	defer d.cond.L.Unlock()
	d.cond.Wait()
}

func (d *fakeDistributor) ClearSnapshot(name string) {
	// Do nothing.
}

func newEndpoints(ips ...string) coreV1.Endpoints {
	addresses := make([]coreV1.EndpointAddress, 0, len(ips))
	for _, ip := range ips {
		addresses = append(addresses, coreV1.EndpointAddress{
			IP: ip,
		})
	}
	return coreV1.Endpoints{
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
	}
}

func newKubeClient(b *testing.B, ki client.Interfaces) kubernetes.Interface {
	b.Helper()
	kubeClient, err := ki.KubeClient()
	if err != nil {
		b.Fatal(err)
	}
	return kubeClient
}

func newSource(b *testing.B, ki client.Interfaces, specs ...schema.ResourceSpec) runtime.Source {
	src, err := kube.New(ki, 0, schema.New(specs...), &converterCfg)
	if err != nil {
		b.Fatal(err)
	}
	if src == nil {
		b.Fatal("Expected non nil source")
	}
	return src
}
