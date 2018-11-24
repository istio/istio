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

package operator

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	pkgtest "istio.io/istio/pkg/test"
)

const configMapName string = "testConfigMapName"
const configMapNameSpace string = "istio-system"

var testCreateControllerCalled bool
var testDeleteControllerCalled bool

func testCreateController(*corev1.ConfigMap) error {
	testCreateControllerCalled = true
	return nil
}

func testDeleteController(string) error {
	testDeleteControllerCalled = true
	return nil
}

func createOperatorConfigMap(k8s *fake.Clientset, t *testing.T) error {
	configMap := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: configMapNameSpace,
//			Labels: map[string]string{
//				"istio/crds": "true",
//			},
		},
		Data: map[string]string{"foo": "bar", "spam": "eggs"},
	}

	_, err := k8s.CoreV1().ConfigMaps(configMapNameSpace).Create(&configMap)
	return err
}

func deleteOperatorConfigMap(k8s *fake.Clientset, t *testing.T) error {
	var immediate int64

	t.Logf("sdake deleteOperatorConfigMap")
	return k8s.CoreV1().ConfigMaps(configMapNameSpace).Delete(
		configMapName, &metav1.DeleteOptions{GracePeriodSeconds: &immediate})
}

func mockLoadKubeConfig(kubeconfig []byte) (*clientcmdapi.Config, error) {
	return &clientcmdapi.Config{}, nil
}

func verifyControllerDeleted(t *testing.T, timeoutName string) {
	pkgtest.NewEventualOpts(10*time.Millisecond, 5*time.Second).Eventually(t, timeoutName, func() bool {
		return testDeleteControllerCalled == true
	})
}

func verifyControllerCreated(t *testing.T, timeoutName string) {
	pkgtest.NewEventualOpts(10*time.Millisecond, 5*time.Second).Eventually(t, timeoutName, func() bool {
		return testCreateControllerCalled == true
	})
}

func Test_OperatorController(t *testing.T) {
	LoadKubeConfig = mockLoadKubeConfig
	t.Logf("sdake Test_OperatorController")

	clientset := fake.NewSimpleClientset()

	// Start the operator controller and sleep to allow operator process to start.
	err := StartOperatorController(
		clientset, testCreateController, testDeleteController, configMapNameSpace)
	if err != nil {
		t.Fatalf("Could not start operator controller: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Create the configmap that is converted into a CRD
	err = createOperatorConfigMap(clientset, t)
	if err != nil {
		t.Fatalf("Unexpected error on operator create: %v", err)
	}

	verifyControllerCreated(t, "Create remote operator controller")

	if testDeleteControllerCalled != false {
		t.Fatalf("Test failed on create operator, delete callback function called")
	}

	// Reset test variables and delete the multicluster operator.
	testCreateControllerCalled = false
	testDeleteControllerCalled = false

	err = deleteOperatorConfigMap(clientset, t)
	if err != nil {
		t.Fatalf("Unexpected error on operator delete: %v", err)
	}

	// Test - Verify that the remote controller has been removed.
	verifyControllerDeleted(t, "delete remote operator controller")

	// Test
	if testCreateControllerCalled != false {
		t.Fatalf("Test failed on delete operator, create callback function called")
	}
}
