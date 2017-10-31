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

package main

import (
	"crypto/x509"
	"flag"
	"fmt"
	"time"

	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/pki/ca/controller"
	"istio.io/istio/security/pkg/pki/testutil"

	"k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

const (
	integrationTestNamespacePrefix = "istio-ca-integration-"
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second
)

var (
	rootCmd = &cobra.Command{
		Run:     runTests,
		PreRun:  initializeIntegrationTest,
		PostRun: cleanUpIntegrationTest,
	}

	opts options
)

type options struct {
	containerHub   string
	containerImage string
	containerTag   string
	clientset      kubernetes.Interface
	kubeconfig     string
	namespace      string
}

func init() {
	flags := rootCmd.Flags()
	flags.StringVar(&opts.containerHub, "hub", "", "Docker hub that the Istio CA image is hosted")
	flags.StringVar(&opts.containerImage, "image", "", "Name of Istio CA image")
	flags.StringVar(&opts.containerTag, "tag", "", "Tag for Istio CA image")
	flags.StringVarP(&opts.kubeconfig, "kube-config", "k", "~/.kube/config", "path to kubeconfig file")

	cmd.InitializeFlags(rootCmd)
}

func main() {
	// HACKHACK: let `flag.Parsed()` return true to prevent glog from emitting errors
	flag.CommandLine = flag.NewFlagSet("", flag.ContinueOnError)
	if err := flag.CommandLine.Parse([]string{}); err != nil {
		glog.Fatal(err)
	}

	if err := rootCmd.Execute(); err != nil {
		glog.Fatal(err)
	}
}

func initializeIntegrationTest(cmd *cobra.Command, args []string) {
	opts.clientset = createClientset(opts.kubeconfig)
	opts.namespace = createTestNamespace(opts.clientset)

	deployIstioCA(opts.clientset)
}

func runTests(cmd *cobra.Command, args []string) {
	// Test the existence of istio.default secret.
	if s, err := waitForSecretExist("istio.default", secretWaitTime); err != nil {
		glog.Fatal(err)
	} else {
		glog.Infof(`Secret "istio.default" is correctly created`)

		expectedID := fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/default", s.GetNamespace())
		examineSecret(s, expectedID)
	}

	// Delete the secret.
	do := &metav1.DeleteOptions{}
	if err := opts.clientset.CoreV1().Secrets(opts.namespace).Delete("istio.default", do); err != nil {
		glog.Fatal(err)
	} else {
		glog.Info(`Secret "istio.default" has been deleted`)
	}

	// Test that the deleted secret is re-created properly.
	if _, err := waitForSecretExist("istio.default", secretWaitTime); err != nil {
		glog.Fatal(err)
	} else {
		glog.Infof(`Secret "istio.default" is correctly re-created`)
	}
}

func cleanUpIntegrationTest(cmd *cobra.Command, args []string) {
	deleteTestNamespace(opts.clientset)
}

func createClientset(kubeconfig string) *kubernetes.Clientset {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		glog.Fatalf("failed to create config object from kube-config file: %q (error: %v)", kubeconfig, err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("failed to create clientset object (error: %v)", err)
	}

	return clientset
}

func createTestNamespace(clientset kubernetes.Interface) string {
	template := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: integrationTestNamespacePrefix,
		},
	}
	namespace, err := clientset.Core().Namespaces().Create(template)
	if err != nil {
		glog.Fatalf("failed to create a namespace (error: %v)", err)
	}

	name := namespace.GetName()
	glog.Infof("Namespace %v is created", name)
	return name
}

func deleteTestNamespace(clientset kubernetes.Interface) {
	name := opts.namespace
	if err := clientset.CoreV1().Namespaces().Delete(name, &metav1.DeleteOptions{}); err != nil {
		glog.Fatalf("failed to delete namespace %q (error: %v)", name, err)
	}
	glog.Infof("Namespace %v is deleted", name)
}

func deployIstioCA(clientset kubernetes.Interface) {
	// Create Role
	role := rbac.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ca-role",
			Namespace: opts.namespace,
		},
		Rules: []rbac.PolicyRule{
			{
				Verbs:     []string{"create", "get", "watch", "list", "update"},
				APIGroups: []string{"core", ""},
				Resources: []string{"secrets"},
			},
			{
				Verbs:     []string{"get", "watch", "list"},
				APIGroups: []string{"core", ""},
				Resources: []string{"serviceaccounts"},
			},
		},
	}
	if _, err := clientset.RbacV1beta1().Roles(opts.namespace).Create(&role); err != nil {
		glog.Fatalf("failed to create role (error: %v)", err)
	}
	// Create RoleBinding
	rolebinding := rbac.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-ca-role-binding",
			Namespace: opts.namespace,
		},
		Subjects: []rbac.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "default",
				Namespace: opts.namespace,
			},
		},
		RoleRef: rbac.RoleRef{
			Kind:     "Role",
			Name:     "istio-ca-role",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	if _, err := clientset.RbacV1beta1().RoleBindings(opts.namespace).Create(&rolebinding); err != nil {
		glog.Fatalf("failed to create rolebinding (error: %v)", err)
	}
	// Create Deployment
	envVar := v1.EnvVar{
		Name: "NAMESPACE",
		ValueFrom: &v1.EnvVarSource{
			FieldRef: &v1.ObjectFieldSelector{
				APIVersion: "v1",
				FieldPath:  "metadata.namespace",
			},
		},
	}
	container := v1.Container{
		Env:   []v1.EnvVar{envVar},
		Name:  "istio-ca-container",
		Image: fmt.Sprintf("%v/%v:%v", opts.containerHub, opts.containerImage, opts.containerTag),
	}
	spec := v1.PodSpec{
		Containers: []v1.Container{container},
	}
	uuid := string(uuid.NewUUID())
	objectMeta := metav1.ObjectMeta{
		Labels: map[string]string{"uuid": uuid},
		Name:   "istio-ca",
	}
	pod := &v1.Pod{
		ObjectMeta: objectMeta,
		Spec:       spec,
	}

	if _, err := clientset.CoreV1().Pods(opts.namespace).Create(pod); err != nil {
		glog.Fatalf("failed to deploy Istio CA (error: %v)", err)
	}

	if err := waitForPodRunning(uuid, 60*time.Second); err != nil {
		glog.Fatal(err)
	}
}

// This method examines the content of an Istio secret to make sure that
// * Secret type is correctly set;
// * Key, certificate and CA root are correctly saved in the data section;
func examineSecret(secret *v1.Secret, expectedID string) {
	if secret.Type != controller.IstioSecretType {
		glog.Fatalf(`Unexpected value for the "type" annotation: expecting %v but got %v`,
			controller.IstioSecretType, secret.Type)
	}

	for _, key := range []string{controller.CertChainID, controller.RootCertID, controller.PrivateKeyID} {
		if _, exists := secret.Data[key]; !exists {
			glog.Fatalf("%v does not exist in the data section", key)
		}
	}

	key := secret.Data[controller.PrivateKeyID]
	cert := secret.Data[controller.CertChainID]
	root := secret.Data[controller.RootCertID]
	verifyFields := &testutil.VerifyFields{
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		IsCA:        false,
	}
	if err := testutil.VerifyCertificate(key, cert, root, expectedID, verifyFields); err != nil {
		glog.Fatalf("Certificate verification failed: %v", err)
	}
}

func waitForPodRunning(uuid string, timeToWait time.Duration) error {
	selectors := labels.Set{"uuid": uuid}.AsSelectorPreValidated()
	listOptions := metav1.ListOptions{
		LabelSelector: selectors.String(),
	}
	watch, err := opts.clientset.CoreV1().Pods(opts.namespace).Watch(listOptions)
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
				glog.Infof("Pod %v/%v is in Running phase", opts.namespace, pod.GetName())
				return nil
			}
		case <-time.After(timeToWait - time.Since(startTime)):
			return fmt.Errorf("pod is not in running phase within %v", timeToWait)
		}
	}
}

func waitForSecretExist(secretName string, timeToWait time.Duration) (*v1.Secret, error) {
	watch, err := opts.clientset.CoreV1().Secrets(opts.namespace).Watch(metav1.ListOptions{})
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
			return nil, fmt.Errorf("secret %v/%v did not become existent within %v", opts.namespace, secretName, timeToWait)
		}
	}
}
