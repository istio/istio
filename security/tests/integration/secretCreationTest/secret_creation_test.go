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
	"flag"
	"os"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/tests/integration"
	"istio.io/istio/tests/integration_old/framework"
)

const (
	testID = "istio_ca_secret_test"
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second
	testEnvName    = "Secret creation and recovery test"
)

var (
	testEnv *integration.SecretTestEnv
)

func TestSecretCreation(t *testing.T) {
	// Test the existence of istio.default secret.
	s, err := integration.WaitForSecretExist(testEnv.ClientSet, testEnv.NameSpace, "istio.default", secretWaitTime)
	if err != nil {
		t.Error(err)
	}

	t.Log(`checking secret "istio.default" is correctly created`)
	if err := integration.ExamineSecret(s); err != nil {
		t.Error(err)
	}

	// Delete the istio.default secret immediately
	if err := integration.DeleteSecret(testEnv.ClientSet, testEnv.NameSpace, "istio.default"); err != nil {
		t.Error(err)
	}

	t.Log(`secret "istio.default" has been deleted`)

	// Test that the deleted secret is re-created properly.
	if _, err := integration.WaitForSecretExist(testEnv.ClientSet, testEnv.NameSpace, "istio.default",
		secretWaitTime); err != nil {
		t.Error(err)
	}
	t.Log(`checking secret "istio.default" is correctly re-created`)
}

func TestMain(m *testing.M) {
	kubeconfig := flag.String("kube-config", "", "path to kubeconfig file")
	hub := flag.String("hub", "", "Docker hub that the Istio CA image is hosted")
	tag := flag.String("tag", "", "Tag for Istio CA image")

	flag.Parse()

	testEnv = integration.NewSecretTestEnv(testEnvName, *kubeconfig, *hub, *tag)

	if testEnv == nil {
		log.Error("test environment creation failure")
		// There is no cleanup needed at this point.
		os.Exit(1)
	}

	res := framework.NewTestEnvManager(testEnv, testID).RunTest(m)

	log.Infof("Test result %d in env %s", res, testEnvName)

	os.Exit(res)
}
