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

package bootstrap

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/util"
)

func TestNamespaceController(t *testing.T) {
	client := fake.NewSimpleClientset()
	testdata := map[string]string{"key": "value"}
	nc, err := NewNamespaceController(func() map[string]string {
		return testdata
	}, client.CoreV1())
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	nc.Run(stop)

	createNamespace(t, client, "foo")
	expectConfigMap(t, client, "foo", testdata)

	newData := map[string]string{"key": "value", "foo": "bar"}
	if err := util.InsertDataToConfigMap(client.CoreV1(), metav1.ObjectMeta{Name: CACertNamespaceConfigMap, Namespace: "foo"}, newData); err != nil {
		t.Fatal(err)
	}
	expectConfigMap(t, client, "foo", newData)

	deleteConfigMap(t, client, "foo")
	expectConfigMap(t, client, "foo", testdata)
}

func deleteConfigMap(t *testing.T, client *fake.Clientset, ns string) {
	t.Helper()
	if err := client.CoreV1().ConfigMaps(ns).Delete(CACertNamespaceConfigMap, &metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func createNamespace(t *testing.T, client *fake.Clientset, ns string) {
	t.Helper()
	if _, err := client.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	}); err != nil {
		t.Fatal(err)
	}
}

func expectConfigMap(t *testing.T, client *fake.Clientset, ns string, data map[string]string) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		cm, err := client.CoreV1().ConfigMaps(ns).Get(CACertNamespaceConfigMap, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(cm.Data, data) {
			return fmt.Errorf("data mismatch, expected %+v got %+v", data, cm.Data)
		}
		return nil
	}, retry.Timeout(time.Second*2))
}
