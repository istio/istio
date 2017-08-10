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

	"istio.io/auth/pkg/pki/ca/controller"
	"istio.io/auth/pkg/pki/testutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

const integrationTestNamespacePrefix = "istio-ca-integration-"

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

	addFlags(rootCmd)
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
	// Test the existence of istio.default.secret.
	if s, err := waitForSecretExist("istio.default", 20*time.Second); err != nil {
		glog.Fatal(err)
	} else {
		glog.Infof(`Secret "istio.default" is correctly created`)

		expectedID := fmt.Sprintf("spiffe://cluster.local/ns/%s/sa/default", s.GetNamespace())
		examineSecret(s, expectedID)
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
	if err := clientset.Core().Namespaces().Delete(name, &metav1.DeleteOptions{}); err != nil {
		glog.Fatalf("failed to delete namespace %q (error: %v)", name, err)
	}
	glog.Infof("Namespace %v is deleted", name)
}

func deployIstioCA(clientset kubernetes.Interface) {
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
	spec := v1.PodSpec{Containers: []v1.Container{container}}
	uuid := string(uuid.NewUUID())
	objectMeta := metav1.ObjectMeta{
		Labels: map[string]string{"uuid": uuid},
		Name:   "istio-ca",
	}
	pod := &v1.Pod{
		ObjectMeta: objectMeta,
		Spec:       spec,
	}

	if _, err := clientset.Core().Pods(opts.namespace).Create(pod); err != nil {
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
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
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
	watch, err := opts.clientset.Core().Pods(opts.namespace).Watch(listOptions)
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
	watch, err := opts.clientset.Core().Secrets(opts.namespace).Watch(metav1.ListOptions{})
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

// addFlags carries over glog flags with new defaults
func addFlags(rootCmd *cobra.Command) {
	flag.CommandLine.VisitAll(func(gf *flag.Flag) {
		switch gf.Name {
		case "logtostderr":
			if err := gf.Value.Set("true"); err != nil {
				fmt.Printf("missing logtostderr flag: %v", err)
			}
		case "log_dir", "stderrthreshold":
			// always use stderr for logging
		default:
			rootCmd.PersistentFlags().AddGoFlag(gf)
		}
	})
}
