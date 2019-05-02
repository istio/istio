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

package installer // import "k8s.io/helm/cmd/helm/installer"

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	deploymentName = "tiller-deploy"
	serviceName    = "tiller-deploy"
	secretName     = "tiller-secret"
)

// Uninstall uses Kubernetes client to uninstall Tiller.
func Uninstall(client kubernetes.Interface, opts *Options) error {
	if err := deleteService(client.Core(), opts.Namespace); err != nil {
		return err
	}
	if err := deleteDeployment(client, opts.Namespace); err != nil {
		return err
	}
	return deleteSecret(client.Core(), opts.Namespace)
}

// deleteService deletes the Tiller Service resource
func deleteService(client corev1.ServicesGetter, namespace string) error {
	err := client.Services(namespace).Delete(serviceName, &metav1.DeleteOptions{})
	return ingoreNotFound(err)
}

// deleteDeployment deletes the Tiller Deployment resource
func deleteDeployment(client kubernetes.Interface, namespace string) error {
	policy := metav1.DeletePropagationBackground
	err := client.AppsV1().Deployments(namespace).Delete(deploymentName, &metav1.DeleteOptions{
		PropagationPolicy: &policy,
	})
	return ingoreNotFound(err)
}

// deleteSecret deletes the Tiller Secret resource
func deleteSecret(client corev1.SecretsGetter, namespace string) error {
	err := client.Secrets(namespace).Delete(secretName, &metav1.DeleteOptions{})
	return ingoreNotFound(err)
}

func ingoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
