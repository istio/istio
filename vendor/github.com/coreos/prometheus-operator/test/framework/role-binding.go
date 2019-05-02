// Copyright 2017 The prometheus-operator Authors
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

package framework

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

func CreateRoleBinding(kubeClient kubernetes.Interface, ns string, relativePath string) (finalizerFn, error) {
	finalizerFn := func() error { return DeleteRoleBinding(kubeClient, ns, relativePath) }
	roleBinding, err := parseRoleBindingYaml(relativePath)
	if err != nil {
		return finalizerFn, err
	}

	_, err = kubeClient.RbacV1().RoleBindings(ns).Create(roleBinding)
	return finalizerFn, err
}

func DeleteRoleBinding(kubeClient kubernetes.Interface, ns string, relativePath string) error {
	roleBinding, err := parseRoleBindingYaml(relativePath)
	if err != nil {
		return err
	}

	return kubeClient.RbacV1().RoleBindings(ns).Delete(roleBinding.Name, &metav1.DeleteOptions{})
}

func parseRoleBindingYaml(relativePath string) (*rbacv1.RoleBinding, error) {
	manifest, err := PathToOSFile(relativePath)
	if err != nil {
		return nil, err
	}

	roleBinding := rbacv1.RoleBinding{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&roleBinding); err != nil {
		return nil, err
	}

	return &roleBinding, nil
}
