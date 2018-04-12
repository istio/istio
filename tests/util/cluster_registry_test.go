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
package util

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

/*
func buildLocalClient() (*kubernetes.Clientset, error) {
	rc, err := clientcmd.BuildConfigFromFlags("http://localhost:8080", "")
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(rc)
}

func checkLocalAPIServer() error {

	client, err := buildLocalClient()
	if err != nil {
		return err
	}
	_, err = client.CoreV1().Namespaces().List(metav1.ListOptions{})
	return err
}

func createTestEndpoint(epName, epNamespace string) error {
	client, err := buildLocalClient()
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

func deleteTestEndpoint(epName, epNamespace string) error {
	client, err := buildLocalClient()
	if err != nil {
		return err
	}

	return client.CoreV1().Endpoints(epNamespace).Delete(epName, &metav1.DeleteOptions{})
}

func createTestService(svcName, svcNamespace string) error {
	client, err := buildLocalClient()
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

func ensureTestNamespace(nsName string) error {
	client, err := buildLocalClient()
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

func deleteTestService(svcName, svcNamespace string) error {
	client, err := buildLocalClient()
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
			Port:            18080, // An unused port will be chosen
			GrpcAddr:        ":0",
			EnableCaching:   true,
			EnableProfiling: true,
		},
		//TODO: start mixer first, get its address
		Mesh: bootstrap.MeshArgs{
			MixerAddress:    "istio-mixer.istio-system:9091",
			RdsRefreshDelay: ptypes.DurationProto(10 * time.Millisecond),
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

	if err := checkLocalAPIServer(); err != nil {
		t.Errorf("Local API Server is not running")
	} else {
		t.Log("API Server is running")
	}
}

// Test
func TestLocalPilotStart(t *testing.T) {

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

	if err = ensureTestNamespace("istio-system"); err != nil {
		t.Fatalf("Failed to create namespace with error: %v", err)
	}
	if err = createTestService("app1", "istio-system"); err != nil {
		t.Fatalf("Failed to create service with error: %v", err)
	}

	if err = createTestEndpoint("app1", "istio-system"); err != nil {
		t.Fatalf("Failed to create endpoints with error: %v", err)
	}

	svc, err := pilot.ServiceController.GetService("app1.istio-system.svc.")
	if err != nil {
		t.Fatalf("Faled to get Service with error: %v", err)
	}
	if svc == nil {
		t.Fatalf("Expected to have 1 service, but 0 found")
	}

	_ = deleteTestEndpoint("app1", "istio-system")
	_ = deleteTestService("app1", "istio-system")
}
*/

func getAPIsURLs() ([]string, error) {

	urlPath := os.Getenv("GOPATH")
	if len(urlPath) == 0 {
		return []string{}, fmt.Errorf("GOPATH is not set")
	}
	urlPath = path.Join(urlPath, "out/log")
	if _, err := os.Stat(urlPath); err != nil {
		return []string{}, fmt.Errorf("Problem with location of URL files, error: %v", err)
	}
	var urls []string
	filepath.Walk(urlPath, func(path string, f os.FileInfo, _ error) error {
		if !f.IsDir() {
			if filepath.Ext(path) == ".url" && strings.HasPrefix(f.Name(), "apiserver") {
				b, err := ioutil.ReadFile(path)
				if err == nil {
					urls = append(urls, string(b))
				}
			}
		}
		return nil
	})

	return urls, nil
}

func buildClusterConfigSecret(urls []string) error {

	return nil
}

func prepareEnv(t *testing.T) error {
	var err error
	err = nil

	urls, err := getAPIsURLs()
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return fmt.Errorf("NOURL")
	}
	t.Logf("Retrived URLs : %+v", urls)

	err = buildClusterConfigSecret(urls)

	return err
}
func TestClusterRegistry(t *testing.T) {

	if err := prepareEnv(t); err != nil {
		if err.Error() == "NOURL" {
			t.Skip("No API servers' URLs were found, skipping...")
		}
		t.Fatalf("Failed with error: %v", err)
	}
}
