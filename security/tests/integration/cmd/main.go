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
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/golang/glog"
	"istio.io/istio/security/pkg/pki/ca/controller"
	"istio.io/istio/security/tests/integration"
	"istio.io/istio/tests/integration/framework"
)

const (
	testID = "istio_ca_secret_test"
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second
	// Certificates validation retry
	certValidateRetry      = 10
	certValidationInterval = 1 // Initially wait for 1 second. This value will be increased exponentially on retry
)

func runSecretCreationTests(env *integration.SecretTestEnv) error {
	// Test the existence of istio.default secret.
	s, err := integration.WaitForSecretExist(env.ClientSet, env.NameSpace, "istio.default", secretWaitTime)
	if err != nil {
		return err
	}

	glog.Info(`checking secret "istio.default" is correctly created`)
	if err := integration.ExamineSecret(s); err != nil {
		return err
	}

	// Delete the istio.default secret immediately
	if err := integration.DeleteSecret(env.ClientSet, env.NameSpace, "istio.default"); err != nil {
		return err
	}
	glog.Info(`secret "istio.default" has been deleted`)

	// Test that the deleted secret is re-created properly.
	if _, err := integration.WaitForSecretExist(env.ClientSet, env.NameSpace, "istio.default",
		secretWaitTime); err != nil {
		return err
	}
	glog.Info(`checking secret "istio.default" is correctly re-created`)

	return nil
}

func runCertificatesRotationTests(env *integration.CertRotationTestEnv) error {
	// Create Istio CA with short lifetime certificates
	initialSecret, err := integration.WaitForSecretExist(env.ClientSet, env.NameSpace, "istio.default",
		secretWaitTime)
	if err != nil {
		return err
	}

	// Validate the secret
	if err := integration.ExamineSecret(initialSecret); err != nil {
		return err
	}

	term := certValidationInterval
	for i := 0; i < certValidateRetry; i++ {
		if i > 0 {
			glog.Infof("checking certificate rotation in %v seconds", term)
			time.Sleep(time.Duration(term) * time.Second)
			term = term * 2
		}

		secret, err := integration.WaitForSecretExist(env.ClientSet, env.NameSpace, "istio.default",
			secretWaitTime)
		if err != nil {
			continue
		}

		if err := integration.ExamineSecret(secret); err != nil {
			return err
		}

		if !bytes.Equal(initialSecret.Data[controller.RootCertID], secret.Data[controller.RootCertID]) {
			return fmt.Errorf("root certificates should be match")
		}

		if !bytes.Equal(initialSecret.Data[controller.PrivateKeyID], secret.Data[controller.PrivateKeyID]) &&
			!bytes.Equal(initialSecret.Data[controller.CertChainID], secret.Data[controller.CertChainID]) {
			glog.Infof("certificates were successfully rotated")
			return nil
		}
	}
	return fmt.Errorf("failed to validate certificate rotation")
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

func runNodeAgentCertificateTests(env *integration.NodeAgentTestEnv, rootCert string, certChain string) error {
	orgRootCert, err := readFile(rootCert)
	if err != nil {
		return fmt.Errorf("unable to read original root certificate: %v", rootCert)
	}

	orgCertChain, err := readFile(certChain)
	if err != nil {
		return fmt.Errorf("unable to read original certificate chain: %v", certChain)
	}

	nodeAgentIPAddress, err := env.GetNodeAgentIPAddress()
	if err != nil {
		return fmt.Errorf("external IP address of NodeAgent is not ready")
	}

	term := certValidationInterval
	for i := 0; i < certValidateRetry; i++ {
		if i > 0 {
			glog.Infof("retry checking certificate update and validation in %v seconds", term)
			time.Sleep(time.Duration(term) * time.Second)
			term = term * 2
		}

		certPEM, err := readURI(fmt.Sprintf("http://%v:8080/cert", nodeAgentIPAddress))
		if err != nil {
			glog.Errorf("failed to read the certificate of NodeAgent: %v", err)
			continue
		}

		rootPEM, err := readURI(fmt.Sprintf("http://%v:8080/root", nodeAgentIPAddress))
		if err != nil {
			glog.Errorf("failed to read the root certificate of NodeAgent: %v", err)
			continue
		}

		if orgRootCert != rootPEM {
			return fmt.Errorf("invalid root certificate was downloaded")
		}

		if orgCertChain == certPEM {
			glog.Error("certificate chain was not updated yet")
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

	return fmt.Errorf("failed to check certificate update and validate after %v retry", certValidateRetry)
}

func main() {
	kubeconfig := flag.String("kube-config", "", "path to kubeconfig file")
	hub := flag.String("hub", "", "Docker hub that the Istio CA image is hosted")
	tag := flag.String("tag", "", "Tag for Istio CA image")
	rootCert := flag.String("root-cert", "", "Path to the original root certificate")
	certChain := flag.String("cert-chain", "", "Path to the original workload certificate chain")

	flag.Parse()

	clientset, err := integration.CreateClientset(*kubeconfig)
	if err != nil {
		glog.Fatalf("failed to initialize K8s client: %s\n", err)
	}

	env := integration.NewSecretTestEnv("Secret creation and recovery test", clientset, *hub, *tag)
	if env != nil {
		testEM := framework.NewTestEnvManager(env, testID)
		if err := testEM.StartUp(); err != nil {
			glog.Fatalf("failed to start the environment: %s", err)
		} else {
			glog.Infof("environment %v is ready for testing..", env.GetName())
			err := runSecretCreationTests(env)
			if err != nil {
				glog.Fatal(err)
			}
			glog.Info("fished test..")
		}
		testEM.TearDown()
	}

	envCertRotation := integration.NewCertRotationTestEnv("Certificates rotation test", clientset, *hub, *tag)
	if envCertRotation != nil {
		testEM := framework.NewTestEnvManager(envCertRotation, testID)
		if err := testEM.StartUp(); err != nil {
			glog.Fatalf("failed to start the environment: %s\n", err)
		} else {
			glog.Infof("environment %v is ready for testing..", envCertRotation.GetName())
			err := runCertificatesRotationTests(envCertRotation)
			if err != nil {
				glog.Fatal(err)
			}
			glog.Info("fished test..")
		}
		testEM.TearDown()
	}

	envNodeAgent := integration.NewNodeAgentTestEnv("NodeAgent test", clientset, *hub, *tag)
	if envNodeAgent != nil {
		testEM := framework.NewTestEnvManager(envNodeAgent, testID)
		if err := testEM.StartUp(); err != nil {
			glog.Fatalf("failed to start the environment: %s\n", err)
		} else {
			glog.Infof("environment %v is ready for testing..", envNodeAgent.GetName())

			err := runNodeAgentCertificateTests(envNodeAgent, *rootCert, *certChain)
			if err != nil {
				glog.Fatal(err)
			}

			glog.Info("fished test..")
		}
		testEM.TearDown()
	}
}
