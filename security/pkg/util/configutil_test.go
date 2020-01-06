// Copyright 2020 Istio Authors
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
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

const (
	configMapName = "test-configmap-name"
	namespaceName = "test-ns"
	dataName      = "test-data-name"
)

func TestInsertDataToConfigMap(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "configmaps",
		Version:  "v1",
	}
	testCases := map[string]struct {
		namespace         string
		existingConfigMap *v1.ConfigMap
		data              string
		expectedActions   []ktesting.Action
		expectedErr       string
		client            *fake.Clientset
	}{
		"non-existing ConfigMap": {
			existingConfigMap: nil,
			data:              "test-data",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName,
					configMapName, map[string]string{
						dataName: "test-data"})),
			},
			expectedErr: "",
		},
		"existing ConfigMap": {
			namespace:         namespaceName,
			existingConfigMap: createConfigMap(namespaceName, configMapName, map[string]string{}),
			data:              "test-data",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewUpdateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: "",
		},
		"namespace not specified": {
			namespace:         "",
			existingConfigMap: createConfigMap("", configMapName, map[string]string{}),
			data:              "test-data",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewUpdateAction(gvr, namespaceName, createConfigMap("", configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: "",
		},
		"creation failure for ConfigMap": {
			existingConfigMap: nil,
			data:              "test-data",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: fmt.Sprintf("error when creating configmap %v: no permission to create configmap",
				configMapName),
			client: creatConfigMapDisabledClient(),
		},
	}

	for id, tc := range testCases {
		var client *fake.Clientset
		if tc.client == nil {
			client = fake.NewSimpleClientset()
		} else {
			client = tc.client
		}
		if tc.existingConfigMap != nil {
			if _, err := client.CoreV1().ConfigMaps(tc.namespace).Create(tc.existingConfigMap); err != nil {
				t.Errorf("test case [%s]: failed to create configmap %v", id, err)
			}
		}
		client.ClearActions()
		err := InsertDataToConfigMap(client.CoreV1(), tc.namespace, tc.data, configMapName,
			dataName)
		if err != nil && err.Error() != tc.expectedErr {
			t.Errorf("test case [%s]: actual error (%s) different from expected error (%s).",
				id, err.Error(), tc.expectedErr)
		}
		if err == nil {
			if tc.expectedErr != "" {
				t.Errorf("test case [%s]: expecting error %s but got no error", id, tc.expectedErr)
			} else if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
				t.Errorf("test case [%s]: %v", id, err)
			}
		}
	}
}

func TestInsertDataToConfigMapWithRetry(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "configmaps",
		Version:  "v1",
	}
	testCases := map[string]struct {
		namespace         string
		existingConfigMap *v1.ConfigMap
		data              string
		expectedActions   []ktesting.Action
		expectedErr       string
		client            *fake.Clientset
	}{
		"non-existing ConfigMap": {
			existingConfigMap: nil,
			data:              "test-data",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName,
					configMapName, map[string]string{
						dataName: "test-data"})),
			},
			expectedErr: "",
		},
		"existing ConfigMap": {
			namespace:         namespaceName,
			existingConfigMap: createConfigMap(namespaceName, configMapName, map[string]string{}),
			data:              "test-data",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewUpdateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: "",
		},
		"namespace not specified": {
			namespace:         "",
			existingConfigMap: createConfigMap("", configMapName, map[string]string{}),
			data:              "test-data",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewUpdateAction(gvr, namespaceName, createConfigMap("", configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: "",
		},
		"creation failure for ConfigMap": {
			existingConfigMap: nil,
			data:              "test-data",
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: fmt.Sprintf("error when creating configmap %v: no permission to create configmap",
				configMapName),
			client: creatConfigMapDisabledClient(),
		},
	}

	for id, tc := range testCases {
		var client *fake.Clientset
		if tc.client == nil {
			client = fake.NewSimpleClientset()
		} else {
			client = tc.client
		}
		if tc.existingConfigMap != nil {
			if _, err := client.CoreV1().ConfigMaps(tc.namespace).Create(tc.existingConfigMap); err != nil {
				t.Errorf("test case [%s]: failed to create configmap %v", id, err)
			}
		}
		client.ClearActions()
		err := InsertDataToConfigMapWithRetry(client.CoreV1(), tc.namespace, tc.data, configMapName,
			dataName, 1*time.Second, 2*time.Second)
		if err != nil && err.Error() != tc.expectedErr {
			t.Errorf("test case [%s]: actual error (%s) different from expected error (%s).",
				id, err.Error(), tc.expectedErr)
		}
		if err == nil {
			if tc.expectedErr != "" {
				t.Errorf("test case [%s]: expecting error %s but got no error", id, tc.expectedErr)
			} else if err := checkActions(client.Actions(), tc.expectedActions); err != nil {
				t.Errorf("test case [%s]: %v", id, err)
			}
		}
	}
}

func creatConfigMapDisabledClient() *fake.Clientset {
	client := &fake.Clientset{}
	client.AddReactor("get", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewNotFound(v1.Resource("configmaps"), configMapName)
	})
	client.AddReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewUnauthorized("no permission to create configmap")
	})
	return client
}

// nolint: unparam
func createConfigMap(namespace, configName string, data map[string]string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
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
