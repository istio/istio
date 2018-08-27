// Copyright 2018 Istio Authors.
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

package secretcontroller

import (
	"testing"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const secretName string = "testSecretName"
const secretNameSpace string = "istio-system"

var testCreateControllerCalled bool
var testDeleteControllerCalled bool

func testCreateController(k8sInterface kubernetes.Interface, clusterID string) error {
	testCreateControllerCalled = true
	return nil
}

func testDeleteController(clusterID string) error {
	testDeleteControllerCalled = true
	return nil
}

func createMultiClusterSecret(k8s *fake.Clientset) error {
	data := map[string][]byte{}
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNameSpace,
			Labels: map[string]string{
				"istio/multiCluster": "true",
			},
		},
		Data: map[string][]byte{},
	}

	data["testRemoteCluster"] = []byte("Test")
	secret.Data = data
	_, err := k8s.CoreV1().Secrets(secretNameSpace).Create(&secret)
	return err
}

func deleteMultiClusterSecret(k8s *fake.Clientset) error {
	var immediate int64

	return k8s.CoreV1().Secrets(secretNameSpace).Delete(
		secretName, &metav1.DeleteOptions{GracePeriodSeconds: &immediate})
}

func mockLoadKubeConfig(kubeconfig []byte) (*clientcmdapi.Config, error) {
	return &clientcmdapi.Config{}, nil
}

func Test_SecretController(t *testing.T) {
	loadKubeConfig = mockLoadKubeConfig

	clientset := fake.NewSimpleClientset()

	// Start the secret controller and sleep to allow secret process to start.
	err := StartSecretController(
		clientset, testCreateController, testDeleteController, secretNameSpace)
	if err != nil {
		t.Fatalf("Could not start secret controller: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Create the multicluster secret.
	err = createMultiClusterSecret(clientset)
	if err != nil {
		t.Fatalf("Unexpected error on secret create: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	// Test
	if testCreateControllerCalled != true {
		t.Fatalf("Test failed on create secret, create callback function not called")
	}

	if testDeleteControllerCalled != false {
		t.Fatalf("Test failed on create secret, delete callback function called")
	}

	// Reset test variables and delete the multicluster secret.
	testCreateControllerCalled = false
	testDeleteControllerCalled = false

	err = deleteMultiClusterSecret(clientset)
	if err != nil {
		t.Fatalf("Unexpected error on secret delete: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	// Test
	if testCreateControllerCalled != false {
		t.Fatalf("Test failed on delete secret, create callback function called")
	}

	if testDeleteControllerCalled != true {
		t.Fatalf("Test failed on delete secret, delete callback function not called")
	}
}
