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
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
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
	_ "k8s.io/client-go/plugin/pkg/client/auth" // to avoid 'No Auth Provider found for name "gcp"'

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

const (
	integrationTestNamespacePrefix = "istio-ca-integration-"
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second

	// Certificates validation retry
	certValidateRetry = 10
	certValidationInterval = 1 // Initially wait for 1 second.
	                           // This value will be increased exponentially on retry
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
	org_root_cert  string
	org_cert_chain string
}

func init() {
	flags := rootCmd.Flags()
	flags.StringVar(&opts.containerHub, "hub", "", "Docker hub that the Istio CA image is hosted")
	flags.StringVar(&opts.containerImage, "image", "", "Name of Istio CA image")
	flags.StringVar(&opts.containerTag, "tag", "", "Tag for Istio CA image")
	flags.StringVarP(&opts.kubeconfig, "kube-config", "k", "~/.kube/config", "path to kubeconfig file")
	flags.StringVar(&opts.org_root_cert, "root-cert", "", "Path to the original root ceritificate")
	flags.StringVar(&opts.org_cert_chain, "cert-chain", "", "Path to the original certificate chain")

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

	// Test certificates of NodeAgent were updated and valid
	na_service, err :=
			opts.clientset.CoreV1().Services(opts.namespace).Get("node-agent", metav1.GetOptions{})
	if err != nil {
		glog.Fatalf("failed to get the external IP adddress of node-agent service: %v", err)
	}

	err = waitForNodeAgentCertificateUpdate(fmt.Sprintf("http://%v:%v",
		na_service.Status.LoadBalancer.Ingress[0].IP, 8080))
	if err != nil {
		glog.Fatalf("failed to check certificate update node-agent (err: %v)", err)
	} else {
		glog.Infof("Certificate of NodeAgent was updated and verified successfully")
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

func createService(clientset kubernetes.Interface, name string, port int32,
serviceType v1.ServiceType, pod *v1.Pod) (*v1.Service, error) {
	uuid := string(uuid.NewUUID())
	_, err := clientset.CoreV1().Services(opts.namespace).Create(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"uuid": uuid,
			},
			Name: name,
		},
		Spec: v1.ServiceSpec{
			Type:     serviceType,
			Selector: pod.Labels,
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

	if serviceType == v1.ServiceTypeLoadBalancer {
		err = waitForServiceExternalIPAddress(uuid, 300 * time.Second)
		if err != nil {
			return nil, err
		}
	}

	return clientset.CoreV1().Services(opts.namespace).Get(name, metav1.GetOptions{})
}

func createPod(clientset kubernetes.Interface, image string, name string) (*v1.Pod, error) {
	uuid := string(uuid.NewUUID())

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"uuid":      uuid,
				"pod-group": fmt.Sprintf("%v-pod-group", name),
			},
			Name: name,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				v1.Container{
					Env: []v1.EnvVar{
						v1.EnvVar{
							Name: "NAMESPACE",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									APIVersion: "v1",
									FieldPath:  "metadata.namespace",
								},
							},
						},
					},
					Name:  fmt.Sprintf("%v-pod-container", name),
					Image: image,
				},
			},
		},
	}

	pod, err := clientset.CoreV1().Pods(opts.namespace).Create(pod)
	if err != nil {
		return nil, err
	}

	if err := waitForPodRunning(uuid, 60 * time.Second); err != nil {
		return nil, err
	}

	return clientset.CoreV1().Pods(opts.namespace).Get(name, metav1.GetOptions{})
}

func createRole(clientset kubernetes.Interface) error {
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
	return nil
}

func createRoleBinding(clientset kubernetes.Interface) error {
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
		return err
	}

	return nil
}

func deployIstioCA(clientset kubernetes.Interface) {
	// Create Role
	err := createRole(clientset)
	if err != nil {
		glog.Fatal("failed to create a role (error: %v)", err)
	}

	// Create RoleBinding
	err = createRoleBinding(clientset)
	if err != nil {
		glog.Fatal("failed to create a rolebinding (error: %v)", err)
	}

	// Create Istio CA with self signed certificates
	_, err = createPod(clientset,
		fmt.Sprintf("%v/istio-ca:%v", opts.containerHub, opts.containerTag),
		"istio-ca-self")
	if err != nil {
		glog.Fatalf("failed to deploy Istio CA (error: %v)", err)
	}

	// Create a Istio CA and Service with generated certificates
	ca_pod, err := createPod(clientset,
		fmt.Sprintf("%v/istio-ca-test:%v", opts.containerHub, opts.containerTag),
		"istio-ca-cert")
	if err != nil {
		glog.Fatalf("failed to deploy Istio CA (error: %v)", err)
	}

	// Create the istio-ca service for NodeAgent pod
	_, err = createService(clientset, "istio-ca", 8060, v1.ServiceTypeClusterIP, ca_pod)
	if err != nil {
		glog.Fatalf("failed to deploy Istio CA (error: %v)", err)
	}

	// Create the NodeAgent pod
	na_pod, err := createPod(clientset,
		fmt.Sprintf("%v/node-agent-test:%v", opts.containerHub, opts.containerTag),
		"node-agent-cert")
	if err != nil {
		glog.Fatalf("failed to deploy NodeAgent pod (error: %v)", err)
	}

	// Create the service for NodeAgent pod
	_, err = createService(clientset, "node-agent", 8080, v1.ServiceTypeLoadBalancer, na_pod)
	if err != nil {
		glog.Fatalf("failed to deploy Istio CA (error: %v)", err)
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

func waitForServiceExternalIPAddress(uuid string, timeToWait time.Duration) error {
	selectors := labels.Set{"uuid": uuid}.AsSelectorPreValidated()
	listOptions := metav1.ListOptions{
		LabelSelector: selectors.String(),
	}

	watch, err := opts.clientset.CoreV1().Services(opts.namespace).Watch(listOptions)
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
				glog.Infof("LoadBalancer for %v/%v is ready. IP: %v", opts.namespace, svc.GetName(),
					svc.Status.LoadBalancer.Ingress[0].IP)
				return nil
			}
		case <-time.After(timeToWait - time.Since(startTime)):
			return fmt.Errorf("pod is not in running phase within %v", timeToWait)
		}
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
			return nil, fmt.Errorf("secret %v/%v did not become existent within %v",
				opts.namespace, secretName, timeToWait)
		}
	}
}

func readFile(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readUri(uri string) (string, error) {
	resp, err := http.Get(uri)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", nil
	}

	return string(bodyBytes), nil
}

func waitForNodeAgentCertificateUpdate(appurl string) error {
	max_retry := certValidateRetry
	term := certValidationInterval

	org_root_cert, err := readFile(opts.org_root_cert)
	if err != nil {
		glog.Fatalf("Unable to read original root certificate: %v", opts.org_root_cert)
	}

	org_cert_chain, err := readFile(opts.org_cert_chain)
	if err != nil {
		glog.Fatalf("Unable to read original certificate chain: %v", opts.org_cert_chain)
	}

	for i := 0; i < max_retry; i++ {
		if i > 0 {
			glog.Infof("Retry checking certificate update and validation in %v seconds", term)
			time.Sleep(time.Duration(term) * time.Second)
			term = term * 2
		}

		certPEM, err := readUri(fmt.Sprintf("%v/cert", appurl))
		if err != nil {
			glog.Errorf("%v", err)
			continue
		}

		rootPEM, err := readUri(fmt.Sprintf("%v/root", appurl))
		if err != nil {
			glog.Errorf("%v", err)
			continue
		}

		if org_root_cert != rootPEM {
			glog.Fatal("Invalid root certificate was downloaded")
			continue
		}

		if org_cert_chain == certPEM {
			glog.Fatal("Certificate chain was not updated")
			continue
		}

		roots := x509.NewCertPool()
		ok := roots.AppendCertsFromPEM([]byte(org_root_cert))
		if !ok {
			glog.Error("failed to parse root certificate")
			continue
		}

		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			glog.Error("failed to parse certificate PEM")
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			glog.Errorf("failed to parse certificate: " + err.Error())
			continue
		}

		opts := x509.VerifyOptions{
			Roots: roots,
		}

		if _, err := cert.Verify(opts); err != nil {
			glog.Errorf("failed to verify certificate: %v", err.Error())
			continue
		}

		return nil
	}

	return fmt.Errorf("Failed to check certificate update and validate after %v retry", max_retry)
}
