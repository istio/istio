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

package configmap

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestInsertCATLSRootCert(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "configmaps",
		Version:  "v1",
	}
	testCases := map[string]struct {
		namespace         string
		existingConfigMap *v1.ConfigMap
		certToAdd         string
		expectedActions   []ktesting.Action
		expectedErr       string
	}{
		"Non-existing ConfigMap": {
			existingConfigMap: nil,
			certToAdd:         "ABCD",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
				ktesting.NewCreateAction(gvr, "test-ns", createConfigMap("test-ns", map[string]string{
					"key1": "data1", caTLSRootCertName: "ABCD"})),
			},
			expectedErr: "",
		},
		"Existing ConfigMap": {
			namespace:         "test-ns",
			existingConfigMap: createConfigMap("test-ns", map[string]string{"key1": "data1"}),
			certToAdd:         "ABCD",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
				ktesting.NewUpdateAction(gvr, "test-ns", createConfigMap("test-ns", map[string]string{
					"key1": "data1", caTLSRootCertName: "ABCD"})),
			},
			expectedErr: "",
		},
		"Namespace not specified": {
			namespace:         "",
			existingConfigMap: createConfigMap("", map[string]string{"key1": "data1"}),
			certToAdd:         "ABCD",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
				ktesting.NewUpdateAction(gvr, "test-ns", createConfigMap("", map[string]string{
					"key1": "data1", caTLSRootCertName: "ABCD"})),
			},
			expectedErr: "",
		},
	}

	for id, tc := range testCases {
		client := fake.NewSimpleClientset()
		if tc.existingConfigMap != nil {
			if _, err := client.CoreV1().ConfigMaps(tc.namespace).Create(tc.existingConfigMap); err != nil {
				t.Errorf("Test case [%s]: Failed to update configmap %v", id, err)
			}
		}

		client.ClearActions()
		controller := NewController(tc.namespace, client.CoreV1())

		err := controller.InsertCATLSRootCert(tc.certToAdd)

		if err != nil && err.Error() != tc.expectedErr {
			t.Errorf("Test case [%s]: Get error (%s) different from expected error (%s).",
				id, err.Error(), tc.expectedErr)
		}
		if err == nil {
			if tc.expectedErr != "" {
				t.Errorf("Test case [%s]: Expecting error %s but got no error", id, tc.expectedErr)
			} else if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
				t.Errorf("Test case [%s]: %v", id, err)
			}
		}
	}
}

func TestGetCATLSRootCert(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "configmaps",
		Version:  "v1",
	}
	testCases := map[string]struct {
		namespace         string
		existingConfigMap *v1.ConfigMap
		expectedActions   []ktesting.Action
		expectedCert      string
		expectedErr       string
	}{
		"ConfigMap not exists": {
			existingConfigMap: nil,
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
			},
			expectedErr: "failed to get CA TLS root cert: configmaps \"istio-security\" not found",
		},
		"Cert not exists": {
			namespace:         "test-ns",
			existingConfigMap: createConfigMap("", map[string]string{"key1": "data1"}),
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
			},
			expectedErr: "failed to get CA TLS root cert from configmap istio-security:caTLSRootCert",
		},
		"Cert exists": {
			namespace: "test-ns",
			existingConfigMap: createConfigMap("test-ns", map[string]string{
				"key1": "data1", caTLSRootCertName: "TEST_CERT"}),
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
			},
			expectedCert: "TEST_CERT",
			expectedErr:  "",
		},
		"Cert exists, empty namespace": {
			namespace:         "",
			existingConfigMap: createConfigMap("", map[string]string{"key1": "data1", caTLSRootCertName: "TEST_CERT"}),
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "", istioSecurityConfigMapName),
			},
			expectedCert: "TEST_CERT",
			expectedErr:  "",
		},
	}

	for id, tc := range testCases {
		client := fake.NewSimpleClientset()
		if tc.existingConfigMap != nil {
			if _, err := client.CoreV1().ConfigMaps(tc.namespace).Create(tc.existingConfigMap); err != nil {
				t.Errorf("failed to update configmap %v", err)
			}
		}

		client.ClearActions()
		controller := NewController(tc.namespace, client.CoreV1())

		cert, err := controller.GetCATLSRootCert()

		if err != nil && err.Error() != tc.expectedErr {
			t.Errorf("Test case [%s]: Get error (%s) different from expected error (%s).",
				id, err.Error(), tc.expectedErr)
		}
		if err == nil {
			if tc.expectedErr != "" {
				t.Errorf("Test case [%s]: Expecting error %s but got no error", id, tc.expectedErr)
			} else {
				if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
					t.Errorf("Test case [%s]: %v", id, err)
				}
				if cert != tc.expectedCert {
					t.Errorf("Test case [%s]: certs not match %s VS (expected) %s", id, cert, tc.expectedCert)
				}
			}
		}
	}
}

func createConfigMap(namespace string, data map[string]string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      istioSecurityConfigMapName,
			Namespace: namespace,
		},
		Data: data,
	}
}

func checkActions(actual, expected []ktesting.Action) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("unexpected number of actions, want %d but got %d", len(expected), len(actual))
	}

	for i, action := range actual {
		expectedAction := expected[i]
		verb := expectedAction.GetVerb()
		resource := expectedAction.GetResource().Resource
		if !action.Matches(verb, resource) {
			return fmt.Errorf("unexpected %dth action, want %q but got %q", i, expectedAction, action)
		}
	}

	return nil
}
