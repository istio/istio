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

package clusterregistry

import (
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pkg/kube/secretcontroller"

	"time"

	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
)

const (
	testSecretName      = "testSecretName"
	testSecretNameSpace = "istio-system"
	WatchedNamespace    = "istio-system"
	DomainSuffix        = "fake_domain"
	ResyncPeriod        = 1 * time.Second
)

var mockserviceController = &aggregate.Controller{}
var clearCacheCalled = false

func mockClearCache() {
	clearCacheCalled = true
}

func createMultiClusterSecret(k8s *fake.Clientset) error {
	data := map[string][]byte{}
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSecretName,
			Namespace: testSecretNameSpace,
			Labels: map[string]string{
				"istio/multiCluster": "true",
			},
		},
		Data: map[string][]byte{},
	}

	data["testRemoteCluster"] = []byte("Test")
	secret.Data = data
	_, err := k8s.CoreV1().Secrets(testSecretNameSpace).Create(&secret)
	return err
}

func deleteMultiClusterSecret(k8s *fake.Clientset) error {
	var immediate int64

	return k8s.CoreV1().Secrets(testSecretNameSpace).Delete(
		testSecretName, &metav1.DeleteOptions{GracePeriodSeconds: &immediate})
}

func mockLoadKubeConfig(kubeconfig []byte) (*clientcmdapi.Config, error) {
	return &clientcmdapi.Config{}, nil
}

func mockCreateInterfaceFromClusterConfig(clusterConfig *clientcmdapi.Config) (kubernetes.Interface, error) {
	return fake.NewSimpleClientset(), nil
}

func Test_KubeSecretController(t *testing.T) {
	secretcontroller.LoadKubeConfig = mockLoadKubeConfig
	secretcontroller.CreateInterfaceFromClusterConfig = mockCreateInterfaceFromClusterConfig

	clientset := fake.NewSimpleClientset()

	mc, err := NewMulticluster(clientset, testSecretNameSpace, WatchedNamespace, DomainSuffix, ResyncPeriod, mockserviceController, mockClearCache)

	if err != nil {
		t.Fatalf("error creating Multicluster object and startign secret controller: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	// Create the multicluster secret. Sleep to allow created remote
	// controller to start and callback add function to be called.
	err = createMultiClusterSecret(clientset)
	if err != nil {
		t.Fatalf("Unexpected error on secret create: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Test
	if len(mc.rkc) != 1 {
		t.Errorf("created %v controller(s), want 1", len(mc.rkc))
	}
	t.Logf("rkc %v", mc.rkc)

	// Delete the mulicluster secret.
	err = deleteMultiClusterSecret(clientset)
	if err != nil {
		t.Fatalf("Unexpected error on secret delete: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	// Test
	if len(mc.rkc) != 0 {
		t.Errorf("deleted remote controller,  %v controllers found, want 0", len(mc.rkc))
	}

	if clearCacheCalled == false {
		t.Errorf("CLear cache not called")
	}
}
