// Copyright 2017 Istio Authors
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

package ingress

import (
	"os"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
)

var (
	pod           = "test"
	serviceIP     = "1.2.3.4"
	hostname      = "foo.bar.com"
	nodeIP        = "10.0.0.2"
	testNamespace = "test"
	resync        = time.Second
)

func makeAnnotatedIngress(annotation string) *extensions.Ingress {
	if annotation == "" {
		return &extensions.Ingress{}
	}

	return &extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				kube.IngressClassAnnotation: annotation,
			},
		},
	}
}

func makeFakeClient() *fake.Clientset {
	return fake.NewSimpleClientset(
		&v1.PodList{Items: []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ingressgateway",
					Namespace: "istio-system",
					Labels: map[string]string{
						"app": "ingressgateway",
					},
				},
				Spec: v1.PodSpec{
					NodeName: "foo_node",
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
			},
		}},
		&v1.ServiceList{Items: []v1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "istio-ingress",
					Namespace: testNamespace,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{{
							IP: serviceIP,
						}},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "istio-ingress-hostname",
					Namespace: testNamespace,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{{
							Hostname: hostname,
						}},
					},
				},
			},
		}},
		&v1.NodeList{Items: []v1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo_node",
				},
				Status: v1.NodeStatus{
					Addresses: []v1.NodeAddress{
						{
							Type:    v1.NodeExternalIP,
							Address: nodeIP,
						},
					},
				},
			},
		}},
	)
}

func makeStatusSyncer(t *testing.T, client kubernetes.Interface) (*StatusSyncer, error) {
	mesh := model.DefaultMeshConfig()
	mesh.IngressService = "istio-ingress"

	oldEnvs := setAndRestoreEnv(t, map[string]string{"POD_NAME": pod, "POD_NAMESPACE": testNamespace})
	// Restore env settings
	defer setAndRestoreEnv(t, oldEnvs)

	return NewStatusSyncer(&mesh, client, testNamespace, kube.ControllerOptions{
		WatchedNamespaces: testNamespace,
		ResyncPeriod:      resync,
	})
}

// setAndRestoreEnv set the envs with given value, and return the old setting.
func setAndRestoreEnv(t *testing.T, inputs map[string]string) map[string]string {
	oldEnvs := map[string]string{}
	for k, v := range inputs {
		oldEnvs[k] = os.Getenv(k)
		if err := os.Setenv(k, v); err != nil {
			t.Error(err)
		}
	}

	return oldEnvs
}

// TestConvertIngressControllerMode ensures that ingress controller mode is converted to the k8s ingress status syncer's
// representation correctly.
func TestConvertIngressControllerMode(t *testing.T) {
	cases := []struct {
		Mode       meshconfig.MeshConfig_IngressControllerMode
		Annotation string
		Ignore     bool
	}{
		{
			Mode:       meshconfig.MeshConfig_DEFAULT,
			Annotation: "",
			Ignore:     true,
		},
		{
			Mode:       meshconfig.MeshConfig_DEFAULT,
			Annotation: "istio",
			Ignore:     true,
		},
		{
			Mode:       meshconfig.MeshConfig_DEFAULT,
			Annotation: "nginx",
			Ignore:     false,
		},
		{
			Mode:       meshconfig.MeshConfig_STRICT,
			Annotation: "",
			Ignore:     false,
		},
		{
			Mode:       meshconfig.MeshConfig_STRICT,
			Annotation: "istio",
			Ignore:     true,
		},
		{
			Mode:       meshconfig.MeshConfig_STRICT,
			Annotation: "nginx",
			Ignore:     false,
		},
	}

	for _, c := range cases {
		ingressClass, defaultIngressClass := convertIngressControllerMode(c.Mode, "istio")

		ing := makeAnnotatedIngress(c.Annotation)
		if ignore := classIsValid(ing, ingressClass, defaultIngressClass); ignore != c.Ignore {
			t.Errorf("convertIngressControllerMode(%q, %q), with Ingress annotation %q => "+
				"Got ignore %v, want %v", c.Mode, "istio", c.Annotation, c.Ignore, ignore)
		}
	}
}

func TestRunningAddresses(t *testing.T) {
	t.Run("service", testRunningAddressesWithService)
	t.Run("hostname", testRunningAddressesWithHostname)
}

func testRunningAddressesWithService(t *testing.T) {
	client := makeFakeClient()
	syncer, err := makeStatusSyncer(t, client)
	if err != nil {
		t.Fatal(err)
	}

	address, err := syncer.runningAddresses(testNamespace)
	if err != nil {
		t.Fatal(err)
	}

	if len(address) != 1 || address[0] != serviceIP {
		t.Errorf("Address is not correctly set to service ip")
	}
}

func testRunningAddressesWithHostname(t *testing.T) {
	client := makeFakeClient()
	syncer, err := makeStatusSyncer(t, client)
	if err != nil {
		t.Fatal(err)
	}

	syncer.ingressService = "istio-ingress-hostname"

	address, err := syncer.runningAddresses(testNamespace)
	if err != nil {
		t.Fatal(err)
	}

	if len(address) != 1 || address[0] != hostname {
		t.Errorf("Address is not correctly set to hostname")
	}
}

func TestRunningAddressesWithPod(t *testing.T) {
	ingressNamespace = "istio-system" // it is set in real pilot on newController.
	client := makeFakeClient()
	syncer, err := makeStatusSyncer(t, client)
	if err != nil {
		t.Fatal(err)
	}

	syncer.ingressService = ""

	address, err := syncer.runningAddresses(ingressNamespace)
	if err != nil {
		t.Fatal(err)
	}

	if len(address) != 1 || address[0] != nodeIP {
		t.Errorf("Address is not correctly set to node ip %v %v", address, nodeIP)
	}
}
