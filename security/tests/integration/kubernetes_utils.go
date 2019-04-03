// Copyright 2017 Istio Authors
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

package integration

import (
	"crypto/x509"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // to avoid 'No Auth Provider found for name "gcp"'

	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/pkg/pki/util"
)

var (
	immediate int64
)

const (
	testNamespacePrefix   = "istio-ca-integration-"
	kubernetesWaitTimeout = 300 * time.Second
)

// CreateClientset creates a new Clientset for the given kubeconfig.
func CreateClientset(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create config object from kube-config file: %q (error: %v)",
			kubeconfig, err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset object (error: %v)", err)
	}

	return clientset, nil
}

// createTestNamespace creates a namespace for test. Returns name of the namespace on success, and error if there is any.
func createTestNamespace(clientset kubernetes.Interface, prefix string) (string, error) {
	template := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix,
		},
	}
	namespace, err := clientset.CoreV1().Namespaces().Create(template)
	if err != nil {
		return "", fmt.Errorf("failed to create a namespace (error: %v)", err)
	}

	log.Infof("namespace %v is created", namespace.GetName())

	// Create Role
	err = createIstioCARole(clientset, namespace.GetName())
	if err != nil {
		_ = deleteTestNamespace(clientset, namespace.GetName())
		return "", fmt.Errorf("failed to create a role (error: %v)", err)
	}

	// Create RoleBinding
	err = createIstioCARoleBinding(clientset, namespace.GetName())
	if err != nil {
		_ = deleteTestNamespace(clientset, namespace.GetName())
		return "", fmt.Errorf("failed to create a rolebinding (error: %v)", err)
	}

	return namespace.GetName(), nil
}

// deleteTestNamespace deletes a namespace for test.
func deleteTestNamespace(clientset kubernetes.Interface, namespace string) error {
	if err := clientset.CoreV1().Namespaces().Delete(namespace, &metav1.DeleteOptions{GracePeriodSeconds: &immediate}); err != nil {
		return fmt.Errorf("failed to delete namespace %q (error: %v)", namespace, err)
	}
	log.Infof("namespace %v is deleted", namespace)
	return nil
}

// createService creates a service object and returns a pointer pointing to this object on success.
func createService(clientset kubernetes.Interface, namespace string, name string, port int32,
	serviceType v1.ServiceType, labels map[string]string, selector map[string]string,
	annotation map[string]string) (*v1.Service, error) {
	_, err := clientset.CoreV1().Services(namespace).Create(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      labels,
			Name:        name,
			Annotations: annotation,
		},
		Spec: v1.ServiceSpec{
			Type:     serviceType,
			Selector: selector,
			Ports: []v1.ServicePort{
				{
					Port: port,
				},
			},
		},
	})

	if err != nil {
		return nil, err
	}

	return clientset.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
}

// deleteService deletes a service.
func deleteService(clientset kubernetes.Interface, namespace string, name string) error {
	return clientset.CoreV1().Services(namespace).Delete(name, &metav1.DeleteOptions{GracePeriodSeconds: &immediate})
}

// createPod creates a pod object and returns a pointer pointing to this object on success.
func createPod(clientset kubernetes.Interface, namespace string, image string, name string,
	labels map[string]string, command []string, args []string) (*v1.Pod, error) {
	env := []v1.EnvVar{
		{
			Name: "NAMESPACE",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.namespace",
				},
			},
		},
	}

	spec := v1.PodSpec{
		Containers: []v1.Container{
			{
				Env:   env,
				Name:  fmt.Sprintf("%v-pod-container", name),
				Image: image,
			},
		},
	}

	if len(command) > 0 {
		spec.Containers[0].Command = command
		if len(args) > 0 {
			spec.Containers[0].Args = args
		}
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
			Name:   name,
		},
		Spec: spec,
	}

	if _, err := clientset.CoreV1().Pods(namespace).Create(pod); err != nil {
		return nil, err
	}

	return clientset.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
}

// deletePod deletes a pod.
func deletePod(clientset kubernetes.Interface, namespace string, name string) error {
	return clientset.CoreV1().Pods(namespace).Delete(name, &metav1.DeleteOptions{GracePeriodSeconds: &immediate})
}

// createIstioCARole creates a role object named "istio-ca-role".
func createIstioCARole(clientset kubernetes.Interface, namespace string) error {
	role := rbac.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ca-role",
			Namespace: namespace,
		},
		Rules: []rbac.PolicyRule{
			{
				Verbs:     []string{"create", "get", "watch", "list", "update"},
				APIGroups: []string{""},
				Resources: []string{"secrets"},
			},
			{
				Verbs:     []string{"get", "watch", "list"},
				APIGroups: []string{""},
				Resources: []string{"serviceaccounts", "services", "pods"},
			},
		},
	}
	if _, err := clientset.RbacV1beta1().Roles(namespace).Create(&role); err != nil {
		return fmt.Errorf("failed to create role (error: %v)", err)
	}
	return nil
}

// createIstioCARoleBinding binds role "istio-ca-role" to default service account.
func createIstioCARoleBinding(clientset kubernetes.Interface, namespace string) error {
	rolebinding := rbac.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ca-role-binding",
			Namespace: namespace,
		},
		Subjects: []rbac.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "default",
				Namespace: namespace,
			},
		},
		RoleRef: rbac.RoleRef{
			Kind:     "Role",
			Name:     "istio-ca-role",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	_, err := clientset.RbacV1beta1().RoleBindings(namespace).Create(&rolebinding)
	return err
}

func waitForServiceExternalIPAddress(clientset kubernetes.Interface, namespace string, uuid string,
	timeToWait time.Duration) error {
	selectors := labels.Set{"uuid": uuid}.AsSelectorPreValidated()
	listOptions := metav1.ListOptions{
		LabelSelector: selectors.String(),
	}

	watch, err := clientset.CoreV1().Services(namespace).Watch(listOptions)
	if err != nil {
		return fmt.Errorf("failed to set up a watch for service (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			svc := event.Object.(*v1.Service)
			if len(svc.Status.LoadBalancer.Ingress) > 0 {
				log.Infof("LoadBalancer for %v/%v is ready. IP: %v", namespace, svc.GetName(),
					svc.Status.LoadBalancer.Ingress[0].IP)
				return nil
			}
		case <-time.After(timeToWait - time.Since(startTime)):
			return fmt.Errorf("pod is not in running phase within %v", timeToWait)
		}
	}
}

func waitForPodRunning(clientset kubernetes.Interface, namespace string, uuid string,
	timeToWait time.Duration) error {
	selectors := labels.Set{"uuid": uuid}.AsSelectorPreValidated()
	listOptions := metav1.ListOptions{
		LabelSelector: selectors.String(),
	}
	watch, err := clientset.CoreV1().Pods(namespace).Watch(listOptions)
	if err != nil {
		return fmt.Errorf("failed to set up a watch for pod (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			pod := event.Object.(*v1.Pod)
			if pod.Status.Phase == v1.PodRunning {
				log.Infof("pod %v/%v is in Running phase", namespace, pod.GetName())
				return nil
			}
		case <-time.After(timeToWait - time.Since(startTime)):
			return fmt.Errorf("pod is not in running phase within %v", timeToWait)
		}
	}
}

func getServiceExternalIPAddress(clientset kubernetes.Interface, namespace string, name string) (string, error) {
	service, err := clientset.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get service: %v err: %v", name, err)
	}

	if len(service.Status.LoadBalancer.Ingress) > 0 {
		return service.Status.LoadBalancer.Ingress[0].IP, nil
	}

	return "", fmt.Errorf("external ip address for the service %v is not ready", name)
}

// WaitForSecretExist takes name of a secret and watches the secret. Returns the requested secret
// if it exists, or error on timeouts.
func WaitForSecretExist(clientset kubernetes.Interface, namespace string, secretName string,
	timeToWait time.Duration) (*v1.Secret, error) {
	watch, err := clientset.CoreV1().Secrets(namespace).Watch(metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to set up watch for secret (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			secret := event.Object.(*v1.Secret)
			if secret.GetName() == secretName {
				return secret, nil
			}
		case <-time.After(timeToWait - time.Since(startTime)):
			return nil, fmt.Errorf("secret %v/%v did not become existent within %v",
				namespace, secretName, timeToWait)
		}
	}
}

// ExamineSecret examines the content of an Istio secret to make sure that
// * Secret type is correctly set;
// * Key, certificate and CA root are correctly saved in the data section;
func ExamineSecret(secret *v1.Secret) error {
	if secret.Type != controller.IstioSecretType {
		return fmt.Errorf(`unexpected value for the "type" annotation: expecting %v but got %v`,
			controller.IstioSecretType, secret.Type)
	}

	for _, key := range []string{controller.CertChainID, controller.RootCertID, controller.PrivateKeyID} {
		if _, exists := secret.Data[key]; !exists {
			return fmt.Errorf("%v does not exist in the data section", key)
		}
	}

	expectedID, err := spiffe.GenSpiffeURI(secret.GetNamespace(), "default")
	if err != nil {
		return err
	}
	verifyFields := &util.VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		IsCA:        false,
		Host:        expectedID,
	}

	if err := util.VerifyCertificate(secret.Data[controller.PrivateKeyID],
		secret.Data[controller.CertChainID], secret.Data[controller.RootCertID],
		verifyFields); err != nil {
		return fmt.Errorf("certificate verification failed: %v", err)
	}

	return nil
}

// DeleteSecret deletes a secret.
func DeleteSecret(clientset kubernetes.Interface, namespace string, name string) error {
	return clientset.CoreV1().Secrets(namespace).Delete(name, &metav1.DeleteOptions{GracePeriodSeconds: &immediate})
}
