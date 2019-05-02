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

func CreateClusterRoleBinding(kubeClient kubernetes.Interface, ns string, relativePath string) (finalizerFn, error) {
	finalizerFn := func() error { return DeleteClusterRoleBinding(kubeClient, relativePath) }
	clusterRoleBinding, err := parseClusterRoleBindingYaml(relativePath)
	if err != nil {
		return finalizerFn, err
	}

	clusterRoleBinding.Subjects[0].Namespace = ns

	_, err = kubeClient.RbacV1().ClusterRoleBindings().Get(clusterRoleBinding.Name, metav1.GetOptions{})

	if err == nil {
		// ClusterRoleBinding already exists -> Update
		_, err = kubeClient.RbacV1().ClusterRoleBindings().Update(clusterRoleBinding)
		if err != nil {
			return finalizerFn, err
		}
	} else {
		// ClusterRoleBinding doesn't exists -> Create
		_, err = kubeClient.RbacV1().ClusterRoleBindings().Create(clusterRoleBinding)
		if err != nil {
			return finalizerFn, err
		}
	}

	return finalizerFn, err
}

func DeleteClusterRoleBinding(kubeClient kubernetes.Interface, relativePath string) error {
	clusterRoleBinding, err := parseClusterRoleYaml(relativePath)
	if err != nil {
		return err
	}

	return kubeClient.RbacV1().ClusterRoleBindings().Delete(clusterRoleBinding.Name, &metav1.DeleteOptions{})
}

func parseClusterRoleBindingYaml(relativePath string) (*rbacv1.ClusterRoleBinding, error) {
	manifest, err := PathToOSFile(relativePath)
	if err != nil {
		return nil, err
	}

	clusterRoleBinding := rbacv1.ClusterRoleBinding{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&clusterRoleBinding); err != nil {
		return nil, err
	}

	return &clusterRoleBinding, nil
}
