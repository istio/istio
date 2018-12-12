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

package controller

import (
	"testing"

	"k8s.io/api/core/v1"
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
				ktesting.NewCreateAction(gvr, "test-ns", createConfigMap(istioSecurityConfigMapName, "test-ns", map[string]string{
					"key1": "data1", caTLSRootCertName: "ABCD"})),
			},
			expectedErr: "",
		},
		"Existing ConfigMap": {
			namespace:         "test-ns",
			existingConfigMap: createConfigMap(istioSecurityConfigMapName, "test-ns", map[string]string{"key1": "data1"}),
			certToAdd:         "ABCD",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
				ktesting.NewUpdateAction(gvr, "test-ns", createConfigMap(istioSecurityConfigMapName, "test-ns", map[string]string{
					"key1": "data1", caTLSRootCertName: "ABCD"})),
			},
			expectedErr: "",
		},
		"Namespace not specified": {
			namespace:         "",
			existingConfigMap: createConfigMap(istioSecurityConfigMapName, "", map[string]string{"key1": "data1"}),
			certToAdd:         "ABCD",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
				ktesting.NewUpdateAction(gvr, "test-ns", createConfigMap(istioSecurityConfigMapName, "", map[string]string{
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
		controller := NewConfigMapController(tc.namespace, client.CoreV1())

		err := controller.InsertCATLSRootCert(tc.certToAdd)

		if err != nil && err.Error() != tc.expectedErr {
			t.Errorf("Test case [%s]: Get error (%s) different from expected error (%s).",
				id, err.Error(), tc.expectedErr)
		}
		if err == nil && tc.expectedErr != "" {
			t.Errorf("Test case [%s]: Expecting error %s but got no error", id, tc.expectedErr)
		}

		if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
			t.Errorf("Test case [%s]: %v", id, err)
		}
	}
}

func GetCATLSRootCert(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "configmaps",
		Version:  "v1",
	}
	testCases := map[string]struct {
		namespace         string
		existingConfigMap *v1.ConfigMap
		certToAdd         string
		expectedActions   []ktesting.Action
		shouldFail        bool
	}{
		"Non-existing ConfigMap": {
			existingConfigMap: nil,
			certToAdd:         "ABCD",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
				ktesting.NewCreateAction(gvr, "test-ns", createConfigMap(istioSecurityConfigMapName, "test-ns", map[string]string{
					"key1": "data1", caTLSRootCertName: "ABCD"})),
			},
			shouldFail: false,
		},
		"Existing ConfigMap": {
			namespace:         "test-ns",
			existingConfigMap: createConfigMap(istioSecurityConfigMapName, "test-ns", map[string]string{"key1": "data1"}),
			certToAdd:         "ABCD",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
				ktesting.NewUpdateAction(gvr, "test-ns", createConfigMap(istioSecurityConfigMapName, "test-ns", map[string]string{
					"key1": "data1", caTLSRootCertName: "ABCD"})),
			},
			shouldFail: false,
		},
		"Namespace not specified": {
			namespace:         "",
			existingConfigMap: createConfigMap(istioSecurityConfigMapName, "", map[string]string{"key1": "data1"}),
			certToAdd:         "ABCD",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, "test-ns", istioSecurityConfigMapName),
				ktesting.NewUpdateAction(gvr, "test-ns", createConfigMap(istioSecurityConfigMapName, "", map[string]string{
					"key1": "data1", caTLSRootCertName: "ABCD"})),
			},
			shouldFail: false,
		},
	}

	for k, tc := range testCases {
		client := fake.NewSimpleClientset()
		if tc.existingConfigMap != nil {
			if _, err := client.CoreV1().ConfigMaps(tc.namespace).Create(tc.existingConfigMap); err != nil {
				t.Errorf("failed to update configmap %v", err)
			}
		}

		client.ClearActions()
		controller := NewConfigMapController(tc.namespace, client.CoreV1())

		err := controller.InsertCATLSRootCert(tc.certToAdd)

		if err != nil {
			t.Errorf("Error: %v", err)
		}
		if err == nil && tc.shouldFail {
			t.Errorf("Expecting error")
		}

		if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
			t.Errorf("Case %q: %s", k, err.Error())
		}
	}
}

func createConfigMap(name, namespace string, data map[string]string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
}
