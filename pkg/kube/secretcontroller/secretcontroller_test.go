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
	"sync/atomic"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	pkgtest "istio.io/istio/pkg/test"
)

const secretName string = "testSecretName"
const secretNamespace string = "istio-system"

var testCreateControllerCalled int32
var testDeleteControllerCalled int32

func testCreateController(_ kubernetes.Interface, _ string) error {
	atomic.StoreInt32(&testCreateControllerCalled, 1)
	return nil
}

func testDeleteController(_ string) error {
	atomic.StoreInt32(&testDeleteControllerCalled, 1)
	return nil
}

func createMultiClusterSecret(k8s *fake.Clientset) error {
	data := map[string][]byte{}
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
			Labels: map[string]string{
				MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{},
	}

	data["testRemoteCluster"] = []byte("Test")
	secret.Data = data
	_, err := k8s.CoreV1().Secrets(secretNamespace).Create(&secret)
	return err
}

func deleteMultiClusterSecret(k8s *fake.Clientset) error {
	var immediate int64

	return k8s.CoreV1().Secrets(secretNamespace).Delete(
		secretName, &metav1.DeleteOptions{GracePeriodSeconds: &immediate})
}

func mockLoadKubeConfig(_ []byte) (*clientcmdapi.Config, error) {
	return &clientcmdapi.Config{}, nil
}

func mockValidateClientConfig(_ clientcmdapi.Config) error {
	return nil
}

func mockCreateInterfaceFromClusterConfig(_ *clientcmdapi.Config) (kubernetes.Interface, error) {
	return fake.NewSimpleClientset(), nil
}

func verifyControllerDeleted(t *testing.T, timeoutName string) {
	pkgtest.NewEventualOpts(10*time.Millisecond, 5*time.Second).Eventually(t, timeoutName, func() bool {
		return atomic.LoadInt32(&testDeleteControllerCalled) == 1
	})
}

func verifyControllerCreated(t *testing.T, timeoutName string) {
	pkgtest.NewEventualOpts(10*time.Millisecond, 5*time.Second).Eventually(t, timeoutName, func() bool {
		return atomic.LoadInt32(&testCreateControllerCalled) == 1
	})
}

func Test_SecretController(t *testing.T) {
	LoadKubeConfig = mockLoadKubeConfig
	ValidateClientConfig = mockValidateClientConfig
	CreateInterfaceFromClusterConfig = mockCreateInterfaceFromClusterConfig

	clientset := fake.NewSimpleClientset()

	// Start the secret controller and sleep to allow secret process to start.
	err := StartSecretController(
		clientset, testCreateController, testDeleteController, secretNamespace)
	if err != nil {
		t.Fatalf("Could not start secret controller: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Create the multicluster secret.
	err = createMultiClusterSecret(clientset)
	if err != nil {
		t.Fatalf("Unexpected error on secret create: %v", err)
	}

	verifyControllerCreated(t, "Create remote secret controller")

	if atomic.LoadInt32(&testDeleteControllerCalled) == 1 {
		t.Fatalf("Test failed on create secret, delete callback function called")
	}

	// Reset test variables and delete the multicluster secret.
	atomic.StoreInt32(&testCreateControllerCalled, 0)
	atomic.StoreInt32(&testDeleteControllerCalled, 0)

	err = deleteMultiClusterSecret(clientset)
	if err != nil {
		t.Fatalf("Unexpected error on secret delete: %v", err)
	}

	// Test - Verify that the remote controller has been removed.
	verifyControllerDeleted(t, "delete remote secret controller")

	// Test
	if atomic.LoadInt32(&testCreateControllerCalled) == 1 {
		t.Fatalf("Test failed on delete secret, create callback function called")
	}
}
