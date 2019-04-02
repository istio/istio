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
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/tests/integration"
	"istio.io/istio/tests/integration_old/framework"
)

const (
	testID = "nodeagent_vm_mode"
	// Certificates validation retry
	certValidateRetry = 10
	// Initially wait for 1 second. This value will be increased exponentially on retry
	certValidationInterval = 1

	testEnvName = "NodeAgent test"
)

type (
	Config struct {
		rootCert  string
		certChain string
	}
)

var (
	testEnv *integration.NodeAgentTestEnv
	config  *Config
)

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

func initialRead(addr string) (root string, certChain string, err error) {
	retry := 10
	for i := 0; i < retry; i++ {
		root, _ = readURI(fmt.Sprintf("http://%v:8080/root", addr))
		certChain, _ = readURI(fmt.Sprintf("http://%v:8080/cert", addr))
		if root != "" && certChain != "" {
			return root, certChain, nil
		}
		log.Infof("Attempt (%v) empty key or cert, retry in 3 seconds\nroot:%v\ncert:\n%v\n",
			i+1, root, certChain)
		time.Sleep(time.Second * 3)
	}
	return "", "", fmt.Errorf("failed to load key certs after %v retries %v", retry, err)
}

// Test that the node agent's root cert is equal to the initial root cert, and the node agent's
// cert chain is updated to be different from the initial cert chain.
func TestNodeAgent(t *testing.T) {
	nodeAgentIPAddress, err := testEnv.GetNodeAgentIPAddress()
	if err != nil {
		t.Errorf("external IP address of NodeAgent is not ready")
	}
	initialRootCert, initialCertChain, err := initialRead(nodeAgentIPAddress)
	if err != nil {
		t.Errorf("failed to read initial key certs %v", err)
	}
	log.Infof("finish loading initial key certs.")
	term := certValidationInterval
	roots := x509.NewCertPool()
	ok := roots.AppendCertsFromPEM([]byte(initialRootCert))
	if !ok {
		t.Errorf("failed to append initial root certificate from PEM: %s", initialRootCert)
	}
	for i := 0; i < certValidateRetry; i++ {

		retrievedCertChain, err := readURI(fmt.Sprintf("http://%v:8080/cert", nodeAgentIPAddress))
		if err != nil {
			t.Errorf("failed to read the certificate of NodeAgent: %v", err)
		}

		retrievedRootCert, err := readURI(fmt.Sprintf("http://%v:8080/root", nodeAgentIPAddress))
		if err != nil {
			t.Errorf("failed to read the root certificate of NodeAgent: %v", err)
		}

		if initialRootCert != retrievedRootCert {
			t.Errorf("invalid root certificate was downloaded:\n%s\nExpected:\n%s", retrievedRootCert, initialRootCert)
		}

		if initialCertChain != retrievedCertChain {
			block, _ := pem.Decode([]byte(retrievedCertChain))
			if block == nil {
				t.Errorf("failed to parse retrieved certificate chain PEM: %s", retrievedCertChain)
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Errorf("failed to parse retrieved x509 certificate: %v", err)
			}
			if _, err := cert.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
				t.Errorf("failed to verify certificate. Error: %v\nCertificate:\n%s", err, retrievedCertChain)
			}
			log.Infof("certificate updated, succeed\n")
			return
		}
		log.Infof("retry checking certificate update and validation in %v seconds", term)
		time.Sleep(time.Duration(term) * time.Second)
		term *= 2
	}
	t.Errorf("failed to check certificate update and validate after %v retries", certValidateRetry)
}

func TestMain(m *testing.M) {
	kubeconfig := flag.String("kube-config", "", "path to kubeconfig file")
	rootCert := flag.String("root-cert", "", "Path to the original root certificate")
	certChain := flag.String("cert-chain", "", "Path to the original workload certificate chain")
	hub := flag.String("hub", "", "Docker hub that the Istio CA image is hosted")
	tag := flag.String("tag", "", "Tag for Istio CA image")

	flag.Parse()

	config = &Config{
		rootCert:  *rootCert,
		certChain: *certChain,
	}

	log.Errorf("%v", config)

	testEnv = integration.NewNodeAgentTestEnv(testEnvName, *kubeconfig, *hub, *tag)

	if testEnv == nil {
		log.Error("test environment creation failure")
		// There is no cleanup needed at this point.
		os.Exit(1)
	}

	res := framework.NewTestEnvManager(testEnv, testID).RunTest(m)

	log.Infof("Test result %d in env %s", res, testEnvName)

	os.Exit(res)
}
