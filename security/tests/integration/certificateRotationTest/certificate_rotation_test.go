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
	"bytes"
	"flag"
	"os"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/k8s/controller"
	"istio.io/istio/security/tests/integration"
	"istio.io/istio/tests/integration_old/framework"
)

const (
	testID = "istio_ca_secret_test"
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second
	// Certificates validation retry
	certValidateRetry = 10
	// Initially wait for 1 second. This value will be increased exponentially on retry
	certValidationInterval = 1
	testEnvName            = "Certificate rotation test"
)

var (
	testEnv *integration.CertRotationTestEnv
)

func TestCertificateRotation(t *testing.T) {
	// Create Istio CA with short lifetime certificates
	initialSecret, err := integration.WaitForSecretExist(testEnv.ClientSet, testEnv.NameSpace, "istio.default",
		secretWaitTime)
	if err != nil {
		t.Error(err)
	}

	// Validate the secret
	if err := integration.ExamineSecret(initialSecret); err != nil {
		t.Error(err)
	}

	term := certValidationInterval
	for i := 0; i < certValidateRetry; i++ {
		if i > 0 {
			t.Logf("checking certificate rotation in %v seconds", term)
			time.Sleep(time.Duration(term) * time.Second)
			term *= 2
		}

		secret, err := integration.WaitForSecretExist(testEnv.ClientSet, testEnv.NameSpace, "istio.default",
			secretWaitTime)
		if err != nil {
			continue
		}

		if err := integration.ExamineSecret(secret); err != nil {
			t.Error(err)
		}

		if !bytes.Equal(initialSecret.Data[controller.RootCertID], secret.Data[controller.RootCertID]) {
			t.Errorf("root certificates should be match")
		}

		if !bytes.Equal(initialSecret.Data[controller.PrivateKeyID], secret.Data[controller.PrivateKeyID]) &&
			!bytes.Equal(initialSecret.Data[controller.CertChainID], secret.Data[controller.CertChainID]) {
			t.Logf("certificates were successfully rotated")

			return
		}
	}
	t.Errorf("failed to validate certificate rotation")
}

func TestMain(m *testing.M) {
	kubeconfig := flag.String("kube-config", "", "path to kubeconfig file")
	hub := flag.String("hub", "", "Docker hub that the Istio CA image is hosted")
	tag := flag.String("tag", "", "Tag for Istio CA image")

	flag.Parse()

	testEnv = integration.NewCertRotationTestEnv(testEnvName, *kubeconfig, *hub, *tag)

	if testEnv == nil {
		log.Error("test environment creation failure")
		// There is no cleanup needed at this point.
		os.Exit(1)
	}

	res := framework.NewTestEnvManager(testEnv, testID).RunTest(m)

	log.Infof("Test result %d in env %s", res, testEnvName)

	os.Exit(res)
}
