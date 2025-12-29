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

package k8s

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/test"
)

const (
	configMapName = "test-configmap-name"
	namespaceName = "test-ns"
	dataName      = "test-data-name"
)

func TestUpdateDataInConfigMap(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "configmaps",
		Version:  "v1",
	}
	caBundle := "test-data"
	testData := map[string]string{
		constants.CACertNamespaceConfigMapDataName: "test-data",
	}
	testCases := []struct {
		name              string
		existingConfigMap *v1.ConfigMap
		expectedActions   []ktesting.Action
		expectedErr       string
	}{
		{
			name:        "non-existing ConfigMap",
			expectedErr: "cannot update nil configmap",
		},
		{
			name:              "existing empty ConfigMap",
			existingConfigMap: createConfigMap(namespaceName, configMapName, map[string]string{}),
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName, testData)),
			},
			expectedErr: "",
		},
		{
			name:              "existing nop ConfigMap",
			existingConfigMap: createConfigMap(namespaceName, configMapName, testData),
			expectedActions:   []ktesting.Action{},
			expectedErr:       "",
		},
		{
			name:              "existing with other keys",
			existingConfigMap: createConfigMap(namespaceName, configMapName, map[string]string{"foo": "bar"}),
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{"test-key": "test-data", "foo": "bar"})),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kc := kube.NewFakeClient()
			fake := kc.Kube().(*fake.Clientset)
			configmaps := kclient.New[*v1.ConfigMap](kc)
			if tc.existingConfigMap != nil {
				if _, err := configmaps.Create(tc.existingConfigMap); err != nil {
					t.Errorf("failed to create configmap %v", err)
				}
			}
			fake.ClearActions()
			err := updateDataInConfigMap(configmaps, tc.existingConfigMap, constants.CACertNamespaceConfigMapDataName, []byte(caBundle))
			if err != nil && err.Error() != tc.expectedErr {
				t.Errorf("actual error (%s) different from expected error (%s).", err.Error(), tc.expectedErr)
			}
			if err == nil {
				if tc.expectedErr != "" {
					t.Errorf("expecting error %s but got no error", tc.expectedErr)
				} else if err := checkActions(fake.Actions(), tc.expectedActions); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func TestInsertDataToConfigMap(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Resource: "configmaps",
		Version:  "v1",
	}
	caBundle := []byte("test-data")
	testData := map[string]string{
		constants.CACertNamespaceConfigMapDataName: "test-data",
	}
	testCases := []struct {
		name              string
		meta              metav1.ObjectMeta
		existingConfigMap *v1.ConfigMap
		caBundle          []byte
		expectedActions   []ktesting.Action
		expectedErr       string
		clientMod         func(*fake.Clientset)
	}{
		{
			name:              "non-existing ConfigMap",
			existingConfigMap: nil,
			caBundle:          caBundle,
			meta:              metav1.ObjectMeta{Namespace: namespaceName, Name: configMapName},
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName,
					configMapName, testData)),
			},
			expectedErr: "",
		},
		{
			name:              "existing ConfigMap",
			meta:              metav1.ObjectMeta{Namespace: namespaceName, Name: configMapName},
			existingConfigMap: createConfigMap(namespaceName, configMapName, map[string]string{}),
			caBundle:          caBundle,
			expectedActions: []ktesting.Action{
				ktesting.NewUpdateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName, testData)),
			},
			expectedErr: "",
		},
		{
			name:              "creation failure for ConfigMap",
			existingConfigMap: nil,
			caBundle:          caBundle,
			meta:              metav1.ObjectMeta{Namespace: namespaceName, Name: configMapName},
			expectedActions: []ktesting.Action{
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewGetAction(gvr, namespaceName, configMapName),
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: fmt.Sprintf("error when creating configmap %v: no permission to create configmap",
				configMapName),
			clientMod: createConfigMapDisabledClient,
		},
		{
			name:              "creation: concurrently created by other client",
			existingConfigMap: nil,
			caBundle:          caBundle,
			meta:              metav1.ObjectMeta{Namespace: namespaceName, Name: configMapName},
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: "",
			clientMod:   createConfigMapAlreadyExistClient,
		},
		{
			name:              "creation: namespace is deleting",
			existingConfigMap: nil,
			caBundle:          caBundle,
			meta:              metav1.ObjectMeta{Namespace: namespaceName, Name: configMapName},
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: "",
			clientMod:   createConfigMapNamespaceDeletingClient,
		},
		{
			name:              "creation: namespace is not found",
			existingConfigMap: nil,
			caBundle:          caBundle,
			meta:              metav1.ObjectMeta{Namespace: namespaceName, Name: configMapName},
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, namespaceName, createConfigMap(namespaceName, configMapName,
					map[string]string{dataName: "test-data"})),
			},
			expectedErr: "",
			clientMod:   createConfigMapNamespaceDeletedClient,
		},
		{
			name:              "creation: namespace is forbidden",
			existingConfigMap: nil,
			caBundle:          caBundle,
			meta:              metav1.ObjectMeta{Namespace: constants.KubeSystemNamespace, Name: configMapName},
			expectedActions: []ktesting.Action{
				ktesting.NewCreateAction(gvr, constants.KubeSystemNamespace, createConfigMap(constants.KubeSystemNamespace, configMapName, testData)),
			},
			expectedErr: "",
			clientMod:   createConfigMapNamespaceForbidden,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []runtime.Object
			if tc.existingConfigMap != nil {
				objs = []runtime.Object{tc.existingConfigMap}
			}
			kc := kube.NewFakeClient(objs...)
			fake := kc.Kube().(*fake.Clientset)
			configmaps := kclient.New[*v1.ConfigMap](kc)
			if tc.clientMod != nil {
				tc.clientMod(fake)
			}
			kc.RunAndWait(test.NewStop(t))
			fake.ClearActions()
			err := InsertDataToConfigMap(configmaps, tc.meta, constants.CACertNamespaceConfigMapDataName, tc.caBundle)
			if err != nil && err.Error() != tc.expectedErr {
				t.Errorf("actual error (%s) different from expected error (%s).", err.Error(), tc.expectedErr)
			}
			if err == nil {
				if tc.expectedErr != "" {
					t.Errorf("expecting error %s but got no error; actions: %+v", tc.expectedErr, fake.Actions())
				} else if err := checkActions(fake.Actions(), tc.expectedActions); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

func createConfigMapDisabledClient(client *fake.Clientset) {
	client.PrependReactor("get", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewNotFound(v1.Resource("configmaps"), configMapName)
	})
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewUnauthorized("no permission to create configmap")
	})
}

func createConfigMapAlreadyExistClient(client *fake.Clientset) {
	client.PrependReactor("get", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewNotFound(v1.Resource("configmaps"), configMapName)
	})
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewAlreadyExists(v1.Resource("configmaps"), configMapName)
	})
}

func createConfigMapNamespaceDeletingClient(client *fake.Clientset) {
	client.PrependReactor("get", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewNotFound(v1.Resource("configmaps"), configMapName)
	})

	err := errors.NewForbidden(v1.Resource("configmaps"), configMapName,
		fmt.Errorf("unable to create new content in namespace %s because it is being terminated", namespaceName))
	err.ErrStatus.Details.Causes = append(err.ErrStatus.Details.Causes, metav1.StatusCause{
		Type:    v1.NamespaceTerminatingCause,
		Message: fmt.Sprintf("namespace %s is being terminated", namespaceName),
		Field:   "metadata.namespace",
	})
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, err
	})
}

func createConfigMapNamespaceDeletedClient(client *fake.Clientset) {
	client.PrependReactor("get", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewNotFound(v1.Resource("configmaps"), configMapName)
	})

	err := errors.NewNotFound(v1.Resource("namespaces"), namespaceName)
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, err
	})
}

func createConfigMapNamespaceForbidden(client *fake.Clientset) {
	client.PrependReactor("get", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewNotFound(v1.Resource("configmaps"), configMapName)
	})
	client.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, errors.NewForbidden(v1.Resource("configmaps"), configMapName, fmt.Errorf(
			"User \"system:serviceaccount:istio-system:istiod\" cannot create resource \"configmaps\" in API group \"\" in the namespace \"kube-system\""))
	})
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
		return fmt.Errorf("unexpected number of actions, want %d but got %d, %v", len(expected), len(actual), actual)
	}

	for i, action := range actual {
		expectedAction := expected[i]
		verb := expectedAction.GetVerb()
		resource := expectedAction.GetResource().Resource
		if !action.Matches(verb, resource) {
			return fmt.Errorf("unexpected %dth action, want \n%+v but got \n%+v\n%v", i, expectedAction, action, cmp.Diff(expectedAction, action))
		}
	}

	return nil
}

func Test_insertData(t *testing.T) {
	type args struct {
		cm   *v1.ConfigMap
		data map[string]string
	}
	tests := []struct {
		name       string
		args       args
		want       bool
		expectedCM *v1.ConfigMap
	}{
		{
			name: "unchanged",
			args: args{
				cm:   createConfigMap(namespaceName, configMapName, map[string]string{"foo": "bar"}),
				data: nil,
			},
			want:       false,
			expectedCM: createConfigMap(namespaceName, configMapName, map[string]string{"foo": "bar"}),
		},
		{
			name: "unchanged",
			args: args{
				cm:   createConfigMap(namespaceName, configMapName, map[string]string{"foo": "bar"}),
				data: map[string]string{"foo": "bar"},
			},
			want:       false,
			expectedCM: createConfigMap(namespaceName, configMapName, map[string]string{"foo": "bar"}),
		},
		{
			name: "changed",
			args: args{
				cm:   createConfigMap(namespaceName, configMapName, map[string]string{"foo": "bar"}),
				data: map[string]string{"bar": "foo"},
			},
			want:       true,
			expectedCM: createConfigMap(namespaceName, configMapName, map[string]string{"foo": "bar", "bar": "foo"}),
		},
		{
			name: "changed",
			args: args{
				cm:   createConfigMap(namespaceName, configMapName, map[string]string{"foo": "bar"}),
				data: map[string]string{"foo": "foo"},
			},
			want:       true,
			expectedCM: createConfigMap(namespaceName, configMapName, map[string]string{"foo": "foo"}),
		},
		{
			name: "changed",
			args: args{
				cm:   createConfigMap(namespaceName, configMapName, nil),
				data: map[string]string{"bar": "foo"},
			},
			want:       true,
			expectedCM: createConfigMap(namespaceName, configMapName, map[string]string{"bar": "foo"}),
		},
		{
			name: "changed",
			args: args{
				cm:   createConfigMap(namespaceName, configMapName, nil),
				data: nil,
			},
			want:       true,
			expectedCM: createConfigMap(namespaceName, configMapName, nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := insertData(tt.args.cm, tt.args.data); got != tt.want {
				t.Errorf("insertData() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.args.cm.Data, tt.expectedCM.Data) {
				t.Errorf("configmap data: %v, want %v", tt.args.cm.Data, tt.expectedCM)
			}
		})
	}
}
