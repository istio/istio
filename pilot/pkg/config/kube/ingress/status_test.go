// Copyright Istio Authors
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
	"context"
	"os"
	"testing"

	coreV1 "k8s.io/api/core/v1"
	ingress "k8s.io/api/networking/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshapi "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
)

var (
	pod           = "test"
	serviceIP     = "1.2.3.4"
	hostname      = "foo.bar.com"
	nodeIP        = "10.0.0.2"
	testNamespace = "test"
)

func makeAnnotatedIngress(annotation string) *ingress.Ingress {
	if annotation == "" {
		return &ingress.Ingress{}
	}

	return &ingress.Ingress{
		ObjectMeta: metaV1.ObjectMeta{
			Annotations: map[string]string{
				kube.IngressClassAnnotation: annotation,
			},
		},
	}
}

func setupFake(t *testing.T, client kubelib.Client) {
	t.Helper()
	if _, err := client.Kube().CoreV1().Pods("istio-system").Create(context.TODO(), &coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "ingressgateway",
			Namespace: "istio-system",
			Labels: map[string]string{
				"app": "ingressgateway",
			},
		},
		Spec: coreV1.PodSpec{
			NodeName: "foo_node",
		},
		Status: coreV1.PodStatus{
			Phase: coreV1.PodRunning,
		},
	}, metaV1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Kube().CoreV1().Services(testNamespace).Create(context.TODO(), &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "istio-ingress",
			Namespace: testNamespace,
		},
		Status: coreV1.ServiceStatus{
			LoadBalancer: coreV1.LoadBalancerStatus{
				Ingress: []coreV1.LoadBalancerIngress{{
					IP: serviceIP,
				}},
			},
		},
	}, metaV1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Kube().CoreV1().Services(testNamespace).Create(context.TODO(), &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "istio-ingress-hostname",
			Namespace: testNamespace,
		},
		Status: coreV1.ServiceStatus{
			LoadBalancer: coreV1.LoadBalancerStatus{
				Ingress: []coreV1.LoadBalancerIngress{{
					Hostname: hostname,
				}},
			},
		},
	}, metaV1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Kube().CoreV1().Nodes().Create(context.TODO(), &coreV1.Node{
		ObjectMeta: metaV1.ObjectMeta{
			Name: "foo_node",
		},
		Status: coreV1.NodeStatus{
			Addresses: []coreV1.NodeAddress{
				{
					Type:    coreV1.NodeExternalIP,
					Address: nodeIP,
				},
			},
		},
	}, metaV1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func makeStatusSyncer(t *testing.T) (*StatusSyncer, error) {
	m := mesh.DefaultMeshConfig()
	m.IngressService = "istio-ingress"

	oldEnvs := setAndRestoreEnv(t, map[string]string{"POD_NAME": pod, "POD_NAMESPACE": testNamespace})
	// Restore env settings
	defer setAndRestoreEnv(t, oldEnvs)

	client := kubelib.NewFakeClient()
	setupFake(t, client)
	sync, err := NewStatusSyncer(&m, client)
	if err != nil {
		return nil, err
	}
	stop := make(chan struct{})
	client.RunAndWait(stop)
	t.Cleanup(func() {
		close(stop)
	})
	return sync, nil
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
		Annotation string
		Mode       meshapi.MeshConfig_IngressControllerMode
		Ignore     bool
	}{
		{
			Mode:       meshapi.MeshConfig_DEFAULT,
			Annotation: "",
			Ignore:     true,
		},
		{
			Mode:       meshapi.MeshConfig_DEFAULT,
			Annotation: "istio",
			Ignore:     true,
		},
		{
			Mode:       meshapi.MeshConfig_DEFAULT,
			Annotation: "nginx",
			Ignore:     false,
		},
		{
			Mode:       meshapi.MeshConfig_STRICT,
			Annotation: "",
			Ignore:     false,
		},
		{
			Mode:       meshapi.MeshConfig_STRICT,
			Annotation: "istio",
			Ignore:     true,
		},
		{
			Mode:       meshapi.MeshConfig_STRICT,
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
	syncer, err := makeStatusSyncer(t)
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
	syncer, err := makeStatusSyncer(t)
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
	syncer, err := makeStatusSyncer(t)
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
