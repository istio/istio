// Copyright 2018 Istio Authors
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
package local

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
)

func buildLocalClient(apiServerURL string) (*kubernetes.Clientset, error) {
	rc, err := clientcmd.BuildConfigFromFlags(apiServerURL, "")
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(rc)
}

func checkLocalAPIServer(apiServerURL string) error {

	client, err := buildLocalClient(apiServerURL)
	if err != nil {
		return err
	}
	_, err = client.CoreV1().Namespaces().List(metav1.ListOptions{})
	return err
}

func createTestEndpoint(apiServerURL string, epName, epNamespace string) error {
	client, err := buildLocalClient(apiServerURL)
	if err != nil {
		return err
	}

	ep := v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      epName,
			Namespace: epNamespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP: "1.1.1.1",
					},
					{
						IP: "1.1.1.2",
					},
					{
						IP: "1.1.1.3",
					},
				},
				Ports: []v1.EndpointPort{
					{
						Name: epName,
						Port: int32(38081),
					},
				},
			},
		},
	}

	_, err = client.CoreV1().Endpoints(epNamespace).Create(&ep)

	return err
}

func deleteTestEndpoint(apiServerURL string, epName, epNamespace string) error {
	client, err := buildLocalClient(apiServerURL)
	if err != nil {
		return err
	}

	return client.CoreV1().Endpoints(epNamespace).Delete(epName, &metav1.DeleteOptions{})
}

func createTestService(apiServerURL string, svcName, svcNamespace string) error {
	client, err := buildLocalClient(apiServerURL)
	if err != nil {
		return err
	}

	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNamespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name: svcName,
					Port: int32(28081),
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 38081,
					},
				},
			},
		},
	}
	_, err = client.CoreV1().Services("istio-system").Create(s)

	return err
}

func ensureTestNamespace(apiServerURL string, nsName string) error {
	client, err := buildLocalClient(apiServerURL)
	if err != nil {
		return err
	}
	if _, err = client.CoreV1().Namespaces().Get(nsName, metav1.GetOptions{}); err == nil {
		// Namespace already exist, do nothing
		return nil
	}
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	_, err = client.CoreV1().Namespaces().Create(ns)
	return err
}

func deleteTestService(apiServerURL string, svcName, svcNamespace string) error {
	client, err := buildLocalClient(apiServerURL)
	if err != nil {
		return err
	}
	if err := client.CoreV1().Services(svcNamespace).Delete(svcName, &metav1.DeleteOptions{}); err != nil {
		return err
	}

	return nil
}

func initLocalPilot(IstioSrc string) (*bootstrap.Server, error) {

	serverAgrs := bootstrap.PilotArgs{
		Namespace: "istio-system",
		DiscoveryOptions: envoy.DiscoveryServiceOptions{
			HTTPAddr:        ":18080", // An unused port will be chosen
			GrpcAddr:        ":0",
			EnableCaching:   true,
			EnableProfiling: true,
		},
		//TODO: start mixer first, get its address
		Mesh: bootstrap.MeshArgs{
			MixerAddress:    "istio-mixer.istio-system:9091",
			RdsRefreshDelay: types.DurationProto(10 * time.Millisecond),
		},
		Config: bootstrap.ConfigArgs{
			KubeConfig: IstioSrc + "/.circleci/config",
		},
		Service: bootstrap.ServiceArgs{
			Registries: []string{
				string(serviceregistry.KubernetesRegistry)},
		},
	}
	// Create the server for the discovery service.
	discoveryServer, err := bootstrap.NewServer(serverAgrs)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery service: %v", err)
	}

	return discoveryServer, nil
}

func startLocalPilot(s *bootstrap.Server, stop chan struct{}) {
	// Start the server
	_, _ = s.Start(stop)
}

// Test availability of local API Server
func TestLocalAPIServer(t *testing.T) {

	if err := checkLocalAPIServer("http://localhost:8080"); err != nil {
		t.Skip("Local API Server is not running")
	} else {
		t.Log("API Server is running")
	}
}

// Test
func TestLocalPilotStart(t *testing.T) {

	localAPIServerURL := "http://localhost:8080"

	if err := checkLocalAPIServer(localAPIServerURL); err != nil {
		t.Skip("Local API Server is not running")
	}

	// Create the stop channel for all of the servers.
	stop := make(chan struct{})
	defer func() {
		close(stop)
	}()

	istioPath := os.Getenv("GOPATH") + "/src/istio.io/istio"
	pilot, err := initLocalPilot(istioPath)
	if err != nil {
		t.Fatalf("Failed to initialize pilot")
	}

	go func() {
		startLocalPilot(pilot, stop)
	}()

	if err = ensureTestNamespace(localAPIServerURL, "istio-system"); err != nil {
		t.Fatalf("Failed to create namespace with error: %v", err)
	}
	if err = createTestService(localAPIServerURL, "app1", "istio-system"); err != nil {
		t.Fatalf("Failed to create service with error: %v", err)
	}

	if err = createTestEndpoint(localAPIServerURL, "app1", "istio-system"); err != nil {
		t.Fatalf("Failed to create endpoints with error: %v", err)
	}

	svc, err := pilot.ServiceController.GetService("app1.istio-system.svc.")
	if err != nil {
		t.Fatalf("Faled to get Service with error: %v", err)
	}
	if svc == nil {
		t.Fatalf("Expected to have 1 service, but 0 found")
	}

	_ = deleteTestEndpoint(localAPIServerURL, "app1", "istio-system")
	_ = deleteTestService(localAPIServerURL, "app1", "istio-system")
}
