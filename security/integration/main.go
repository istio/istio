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
	if err := flag.CommandLine.Parse([]string{}); err != nil {
		glog.Fatal(err)
	}

	if err := rootCmd.Execute(); err != nil {
		if opts.clientset != nil && len(opts.namespace) > 0 {
			err = utils.DeleteTestNamespace(opts.clientset, opts.namespace)
			if err != nil {
				glog.Errorf("Failed to delete namespace %v : %v", opts.namespace, err)
			}
		}
		glog.Fatal(err)
	}
}

func runTests(cmd *cobra.Command, args []string) error {
	err := runSelfSignedCATests()
	if err != nil {
		return err
	}

	err = runCAForNodeAgentTests()
	if err != nil {
		return err
	}

	return nil
}

func initializeIntegrationTest(cmd *cobra.Command, args []string) error {
	clientset, err := utils.CreateClientset(opts.kubeconfig)
	if err != nil {
		return err
	}
	opts.clientset = clientset

	namespace, err := utils.CreateTestNamespace(opts.clientset, integrationTestNamespacePrefix)
	if err != nil {
		return err
	}
	opts.namespace = namespace

	// Create Role
	err = utils.CreateIstioCARole(opts.clientset, opts.namespace)
	if err != nil {
		utils.DeleteTestNamespace(opts.clientset, opts.namespace)
		return fmt.Errorf("failed to create a role (error: %v)", err)
	}

	// Create RoleBinding
	err = utils.CreateIstioCARoleBinding(opts.clientset, opts.namespace)
	if err != nil {
		utils.DeleteTestNamespace(opts.clientset, opts.namespace)
		return fmt.Errorf("failed to create a rolebinding (error: %v)", err)
	}

	return nil
}

func runSelfSignedCATests() error {
	// Create Istio CA with self signed certificates
	selfSignedCaPod, err := utils.CreatePod(opts.clientset, opts.namespace,
		fmt.Sprintf("%v/istio-ca:%v", opts.containerHub, opts.containerTag), "istio-ca-self")
	if err != nil {
		return fmt.Errorf("failed to deploy Istio CA (error: %v)", err)
	}
	glog.Infof("Successfully created Istio CA pod(%v) with the self-signed-ca option",
		selfSignedCaPod.GetName())

	// Test the existence of istio.default secret.
	secret, err := utils.WaitForSecretExist(opts.clientset, opts.namespace, "istio.default",
		secretWaitTime)
	if err != nil {
		return err
	}
	glog.Info(`Secret "istio.default" is correctly created`)

	expectedID := fmt.Sprintf("spiffe://cluster.local/ns/%secret/sa/default", secret.GetNamespace())
	examineSecret(secret, expectedID)

	// Delete the secret.
	do := &metav1.DeleteOptions{}
	if err = opts.clientset.CoreV1().Secrets(opts.namespace).Delete("istio.default", do); err != nil {
		return err
	}
	glog.Info(`Secret "istio.default" has been deleted`)

	// Test that the deleted secret is re-created properly.
	if _, err = utils.WaitForSecretExist(opts.clientset, opts.namespace, "istio.default",
		secretWaitTime); err != nil {
		return err
	}
	glog.Info(`Secret "istio.default" is correctly re-created`)

	// Delete pods
	err = utils.DeletePod(opts.clientset, opts.namespace, selfSignedCaPod.GetName())
	if err != nil {
		return fmt.Errorf("failed to delete Istio CA (error: %v)", err)
	}

	return nil
}

func runCAForNodeAgentTests() error {
	// Create a Istio CA and Service with generated certificates
	caPod, err := utils.CreatePod(opts.clientset, opts.namespace,
		fmt.Sprintf("%v/istio-ca-test:%v", opts.containerHub, opts.containerTag),
		"istio-ca-cert")
	if err != nil {
		return fmt.Errorf("failed to deploy Istio CA (error: %v)", err)
	}

	// Create the istio-ca service for NodeAgent pod
	caService, err := utils.CreateService(opts.clientset, opts.namespace, "istio-ca", 8060,
		v1.ServiceTypeClusterIP, caPod)
	if err != nil {
		return fmt.Errorf("failed to deploy Istio CA (error: %v)", err)
	}

	// Create the NodeAgent pod
	naPod, err := utils.CreatePod(opts.clientset, opts.namespace,
		fmt.Sprintf("%v/node-agent-test:%v", opts.containerHub, opts.containerTag),
		"node-agent-cert")
	if err != nil {
		return fmt.Errorf("failed to deploy NodeAgent pod (error: %v)", err)
	}

	// Create the service for NodeAgent pod
	naService, err := utils.CreateService(opts.clientset, opts.namespace, "node-agent", 8080,
		v1.ServiceTypeLoadBalancer, naPod)
	if err != nil {
		return fmt.Errorf("failed to deploy Istio CA (error: %v)", err)
	}

	// Test certificates of NodeAgent were updated and valid
	_, err = opts.clientset.CoreV1().Services(opts.namespace).Get("node-agent", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get the external IP adddress of node-agent service: %v", err)
	}

	err = waitForNodeAgentCertificateUpdate(fmt.Sprintf("http://%v:%v",
		naService.Status.LoadBalancer.Ingress[0].IP, 8080))
	if err != nil {
		return fmt.Errorf("failed to check certificate update node-agent (err: %v)", err)
	}
	glog.Info("Certificate of NodeAgent was updated and verified successfully")

	// Delete created services
	err = utils.DeleteService(opts.clientset, opts.namespace, caService.GetName())
	if err != nil {
		return fmt.Errorf("failed to delete CA service: %v (error: %v)", caService.GetName(), err)
	}

	err = utils.DeleteService(opts.clientset, opts.namespace, naService.GetName())
	if err != nil {
		return fmt.Errorf("failed to delete NodeAgent service: %v (error: %v)", naService.GetName(), err)
	}

	// Delete created pods
	err = utils.DeletePod(opts.clientset, opts.namespace, caPod.GetName())
	if err != nil {
		return fmt.Errorf("failed to delete CA pod: %v (error: %v)", caPod.GetName(), err)
	}

	err = utils.DeletePod(opts.clientset, opts.namespace, naPod.GetName())
	if err != nil {
		return fmt.Errorf("failed to delete NodeAgent pod: %v (error: %v)", naPod.GetName(), err)
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
	if err := testutil.VerifyCertificate(key, cert, root, expectedID, verifyFields); err != nil {
		return fmt.Errorf("certificate verification failed: %v", err)
	}

	return nil
}

func readFile(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readURI(uri string) (string, error) {
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
	maxRetry := certValidateRetry
	term := certValidationInterval

	orgRootCert, err := readFile(opts.orgRootCert)
	if err != nil {
		return fmt.Errorf("unable to read original root certificate: %v", opts.orgRootCert)
	}

	orgCertChain, err := readFile(opts.orgCertChain)
	if err != nil {
		return fmt.Errorf("unable to read original certificate chain: %v", opts.orgCertChain)
	}

	for i := 0; i < maxRetry; i++ {
		if i > 0 {
			glog.Infof("Retry checking certificate update and validation in %v seconds", term)
			time.Sleep(time.Duration(term) * time.Second)
			term = term * 2
		}

		certPEM, err := readURI(fmt.Sprintf("%v/cert", appurl))
		if err != nil {
			glog.Errorf("Failed to read the certificate of NodeAgent: %v", err)
			continue
		}

		rootPEM, err := readURI(fmt.Sprintf("%v/root", appurl))
		if err != nil {
			glog.Errorf("Failed to read the root certificate of NodeAgent: %v", err)
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

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse certificate: %v", err)
		}

		opts := x509.VerifyOptions{
			Roots: roots,
		}

		if _, err := cert.Verify(opts); err != nil {
			return fmt.Errorf("failed to verify certificate: %v", err)
		}

		return nil
	}

	return fmt.Errorf("failed to check certificate update and validate after %v retry", maxRetry)
}
