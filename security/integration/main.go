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

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/security/integration/utils"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/pki/ca/controller"
	"istio.io/istio/security/pkg/pki/testutil"
)

const (
	integrationTestNamespacePrefix = "istio-ca-integration-"
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second

	// Certificates validation retry
	certValidateRetry      = 10
	certValidationInterval = 1 // Initially wait for 1 second.
	// This value will be increased exponentially on retry
)

var (
	rootCmd = &cobra.Command{
		PreRunE:  initializeIntegrationTest,
		RunE:     runTests,
		PostRunE: cleanUpIntegrationTest,
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
	orgRootCert    string
	orgCertChain   string
}

func init() {
	flags := rootCmd.Flags()
	flags.StringVar(&opts.containerHub, "hub", "", "Docker hub that the Istio CA image is hosted")
	flags.StringVar(&opts.containerImage, "image", "", "Name of Istio CA image")
	flags.StringVar(&opts.containerTag, "tag", "", "Tag for Istio CA image")
	flags.StringVarP(&opts.kubeconfig, "kube-config", "k", "~/.kube/config", "path to kubeconfig file")
	flags.StringVar(&opts.orgRootCert, "root-cert", "", "Path to the original root ceritificate")
	flags.StringVar(&opts.orgCertChain, "cert-chain", "", "Path to the original workload certificate chain")

	cmd.InitializeFlags(rootCmd)
}

func main() {
	// HACKHACK: let `flag.Parsed()` return true to prevent glog from emitting errors
	flag.CommandLine = flag.NewFlagSet("", flag.ContinueOnError)
	if errors := flag.CommandLine.Parse([]string{}); errors != nil {
		glog.Fatal(errors)
	}

	if errors := rootCmd.Execute(); errors != nil {
		if opts.clientset != nil && len(opts.namespace) > 0 {
			errors = utils.DeleteTestNamespace(opts.clientset, opts.namespace)
			if errors != nil {
				glog.Errorf("Failed to delete namespace %v : %v", opts.namespace, errors)
			}
		}
		glog.Fatal(errors)
	}
}

func runTests(cmd *cobra.Command, args []string) error {
	errors := runSelfSignedCATests()
	if errors != nil {
		return errors
	}

	errors = runCAForNodeAgentTests()
	if errors != nil {
		return errors
	}

	return nil
}

func initializeIntegrationTest(cmd *cobra.Command, args []string) error {
	clientset, errors := utils.CreateClientset(opts.kubeconfig)
	if errors != nil {
		return errors
	}
	opts.clientset = clientset

	namespace, errors := utils.CreateTestNamespace(opts.clientset, integrationTestNamespacePrefix)
	if errors != nil {
		return errors
	}
	opts.namespace = namespace

	// Create Role
	errors = utils.CreateIstioCARole(opts.clientset, opts.namespace)
	if errors != nil {
		utils.DeleteTestNamespace(opts.clientset, opts.namespace)
		return fmt.Errorf("failed to create a role (error: %v)", errors)
	}

	// Create RoleBinding
	errors = utils.CreateIstioCARoleBinding(opts.clientset, opts.namespace)
	if errors != nil {
		utils.DeleteTestNamespace(opts.clientset, opts.namespace)
		return fmt.Errorf("failed to create a rolebinding (error: %v)", errors)
	}

	return nil
}

func runSelfSignedCATests() error {
	// Create Istio CA with self signed certificates
	selfSignedCaPod, errors := utils.CreatePod(opts.clientset, opts.namespace,
		fmt.Sprintf("%v/istio-ca:%v", opts.containerHub, opts.containerTag), "istio-ca-self")
	if errors != nil {
		return fmt.Errorf("failed to deploy Istio CA (error: %v)", errors)
	}
	glog.Infof("Successfully created Istio CA pod(%v) with the self-signed-ca option",
		selfSignedCaPod.GetName())

	// Test the existence of istio.default secret.
	secret, errors := utils.WaitForSecretExist(opts.clientset, opts.namespace, "istio.default",
		secretWaitTime);
	if errors != nil {
		return errors
	}
	glog.Info(`Secret "istio.default" is correctly created`)

	expectedID := fmt.Sprintf("spiffe://cluster.local/ns/%secret/sa/default", secret.GetNamespace())
	examineSecret(secret, expectedID)

	// Delete the secret.
	do := &metav1.DeleteOptions{}
	if errors := opts.clientset.CoreV1().Secrets(opts.namespace).Delete("istio.default", do); errors != nil {
		return errors
	}
	glog.Info(`Secret "istio.default" has been deleted`)

	// Test that the deleted secret is re-created properly.
	if _, errors := utils.WaitForSecretExist(opts.clientset, opts.namespace, "istio.default",
		secretWaitTime); errors != nil {
		return errors
	}
	glog.Info(`Secret "istio.default" is correctly re-created`)

	// Delete pods
	errors = utils.DeletePod(opts.clientset, opts.namespace, selfSignedCaPod.GetName())
	if errors != nil {
		return fmt.Errorf("failed to delete Istio CA (error: %v)", errors)
	}

	return nil
}

func runCAForNodeAgentTests() error {
	// Create a Istio CA and Service with generated certificates
	caPod, errors := utils.CreatePod(opts.clientset, opts.namespace,
		fmt.Sprintf("%v/istio-ca-test:%v", opts.containerHub, opts.containerTag),
		"istio-ca-cert")
	if errors != nil {
		return fmt.Errorf("failed to deploy Istio CA (error: %v)", errors)
	}

	// Create the istio-ca service for NodeAgent pod
	caService, errors := utils.CreateService(opts.clientset, opts.namespace, "istio-ca", 8060,
		v1.ServiceTypeClusterIP, caPod)
	if errors != nil {
		return fmt.Errorf("failed to deploy Istio CA (error: %v)", errors)
	}

	// Create the NodeAgent pod
	naPod, errors := utils.CreatePod(opts.clientset, opts.namespace,
		fmt.Sprintf("%v/node-agent-test:%v", opts.containerHub, opts.containerTag),
		"node-agent-cert")
	if errors != nil {
		return fmt.Errorf("failed to deploy NodeAgent pod (error: %v)", errors)
	}

	// Create the service for NodeAgent pod
	naService, errors := utils.CreateService(opts.clientset, opts.namespace, "node-agent", 8080,
		v1.ServiceTypeLoadBalancer, naPod)
	if errors != nil {
		return fmt.Errorf("failed to deploy Istio CA (error: %v)", errors)
	}

	// Test certificates of NodeAgent were updated and valid
	_, errors = opts.clientset.CoreV1().Services(opts.namespace).Get("node-agent", metav1.GetOptions{})
	if errors != nil {
		return fmt.Errorf("failed to get the external IP adddress of node-agent service: %v", errors)
	}

	errors = waitForNodeAgentCertificateUpdate(fmt.Sprintf("http://%v:%v",
		naService.Status.LoadBalancer.Ingress[0].IP, 8080))
	if errors != nil {
		return fmt.Errorf("failed to check certificate update node-agent (errors: %v)", errors)
	}
	glog.Info("Certificate of NodeAgent was updated and verified successfully")

	// Delete created services
	errors = utils.DeleteService(opts.clientset, opts.namespace, caService.GetName())
	if errors != nil {
		return fmt.Errorf("failed to delete CA service: %v (error: %v)", caService.GetName(), errors)
	}

	errors = utils.DeleteService(opts.clientset, opts.namespace, naService.GetName())
	if errors != nil {
		return fmt.Errorf("failed to delete NodeAgent service: %v (error: %v)", naService.GetName(), errors)
	}

	// Delete created pods
	errors = utils.DeletePod(opts.clientset, opts.namespace, caPod.GetName())
	if errors != nil {
		return fmt.Errorf("failed to delete CA pod: %v (error: %v)", caPod.GetName(), errors)
	}

	errors = utils.DeletePod(opts.clientset, opts.namespace, naPod.GetName())
	if errors != nil {
		return fmt.Errorf("failed to delete NodeAgent pod: %v (error: %v)", naPod.GetName(), errors)
	}

	return nil
}

func cleanUpIntegrationTest(cmd *cobra.Command, args []string) error {
	return utils.DeleteTestNamespace(opts.clientset, opts.namespace)
}

// This method examines the content of an Istio secret to make sure that
// * Secret type is correctly set;
// * Key, certificate and CA root are correctly saved in the data section;
func examineSecret(secret *v1.Secret, expectedID string) error {
	if secret.Type != controller.IstioSecretType {
		return fmt.Errorf(`unexpected value for the "type" annotation: expecting %v but got %v`,
			controller.IstioSecretType, secret.Type)
	}

	for _, key := range []string{controller.CertChainID, controller.RootCertID, controller.PrivateKeyID} {
		if _, exists := secret.Data[key]; !exists {
			return fmt.Errorf("%v does not exist in the data section", key)
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
	if errors := testutil.VerifyCertificate(key, cert, root, expectedID, verifyFields); errors != nil {
		return fmt.Errorf("certificate verification failed: %v", errors)
	}

	return nil
}

func readFile(path string) (string, error) {
	data, errors := ioutil.ReadFile(path)
	if errors != nil {
		return "", errors
	}
	return string(data), nil
}

func readURI(uri string) (string, error) {
	resp, errors := http.Get(uri)
	if errors != nil {
		return "", errors
	}
	defer resp.Body.Close()

	bodyBytes, errors := ioutil.ReadAll(resp.Body)
	if errors != nil {
		return "", nil
	}

	return string(bodyBytes), nil
}

func waitForNodeAgentCertificateUpdate(appurl string) error {
	maxRetry := certValidateRetry
	term := certValidationInterval

	orgRootCert, errors := readFile(opts.orgRootCert)
	if errors != nil {
		return fmt.Errorf("unable to read original root certificate: %v", opts.orgRootCert)
	}

	orgCertChain, errors := readFile(opts.orgCertChain)
	if errors != nil {
		return fmt.Errorf("unable to read original certificate chain: %v", opts.orgCertChain)
	}

	for i := 0; i < maxRetry; i++ {
		if i > 0 {
			glog.Infof("Retry checking certificate update and validation in %v seconds", term)
			time.Sleep(time.Duration(term) * time.Second)
			term = term * 2
		}

		certPEM, errors := readURI(fmt.Sprintf("%v/cert", appurl))
		if errors != nil {
			glog.Errorf("Failed to read the certificate of NodeAgent: %v", errors)
			continue
		}

		rootPEM, errors := readURI(fmt.Sprintf("%v/root", appurl))
		if errors != nil {
			glog.Errorf("Failed to read the root certificate of NodeAgent: %v", errors)
			continue
		}

		if orgRootCert != rootPEM {
			return fmt.Errorf("invalid root certificate was downloaded")
		}

		if orgCertChain == certPEM {
			glog.Error("Certificate chain was not updated yet")
			continue
		}

		roots := x509.NewCertPool()
		ok := roots.AppendCertsFromPEM([]byte(orgRootCert))
		if !ok {
			return fmt.Errorf("failed to parse root certificate")
		}

		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			return fmt.Errorf("failed to parse certificate PEM")
		}

		cert, errors := x509.ParseCertificate(block.Bytes)
		if errors != nil {
			return fmt.Errorf("failed to parse certificate: %v", errors)
		}

		opts := x509.VerifyOptions{
			Roots: roots,
		}

		if _, errors := cert.Verify(opts); errors != nil {
			return fmt.Errorf("failed to verify certificate: %v", errors)
		}

		return nil
	}

	return fmt.Errorf("failed to check certificate update and validate after %v retry", maxRetry)
}
