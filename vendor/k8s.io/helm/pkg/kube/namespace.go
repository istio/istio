/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kube // import "k8s.io/helm/pkg/kube"

import (
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func createNamespace(client kubernetes.Interface, namespace string) error {
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"name": namespace,
			},
		},
	}
	_, err := client.CoreV1().Namespaces().Create(ns)
	return err
}

func getNamespace(client kubernetes.Interface, namespace string) (*v1.Namespace, error) {
	return client.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
}

func ensureNamespace(client kubernetes.Interface, namespace string) error {
	_, err := getNamespace(client, namespace)
	if err != nil && errors.IsNotFound(err) {
		err = createNamespace(client, namespace)

		// If multiple commands which run `ensureNamespace` are run in
		// parallel, then protect against the race condition in which
		// the namespace did not exist when `getNamespace` was executed,
		// but did exist when `createNamespace` was executed. If that
		// happens, we can just proceed as normal.
		if errors.IsAlreadyExists(err) {
			return nil
		}
	}
	return err
}
