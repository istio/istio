// Copyright 2018 Istio Authors
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

// Package galley defines integration tests that validate working galley
// functionality in context of a test Istio-enabled cluster.
package galley

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	yamlExtension = "yaml"
)

type testConfig struct {
	*framework.CommonConfig
	configDir string
}

var (
	tc *testConfig
)

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("galley_test")
	if err != nil {
		return err
	}
	tc = &testConfig{CommonConfig: cc}

	// TODO - enforce test constraints
	if f := flag.Lookup("use_galley_config_validator"); f == nil || f.Value.String() != "true" {
		return errors.New("--use_galley_config_validator flag must be set")
	}
	if f := flag.Lookup("cluster_wide"); f == nil || f.Value.String() != "true" {
		return errors.New("--cluster_wide flag must be set")
	}
	return nil
}

func TestMain(m *testing.M) {
	flag.Parse()
	if err := framework.InitLogging(); err != nil {
		log.Error("cannot setup logging")
		os.Exit(-1)
	}
	if err := setTestConfig(); err != nil {
		log.Errorf("could not create TestConfig: %v", err)
		os.Exit(-1)
	}
	os.Exit(tc.RunTest(m))
}

func applyGalleyConfig(ruleName string) error {
	return doGalleyConfig(ruleName, util.KubeApplyContentSilent)
}

type kubeDo func(namespace string, contents string, kubeconfig string) error

func doGalleyConfig(configName string, do kubeDo) error {
	contents, err := ioutil.ReadFile(filepath.Join(tc.configDir, configName))
	if err != nil {
		return err
	}
	return do(tc.Kube.Namespace, string(contents), tc.Kube.KubeConfig)
}

const (
	waitForValidatingWebhookConfigTimeout   = 5 * time.Second
	waitForValidatingWebhookConfigCheckFreq = 100 * time.Millisecond
)

func TestValidation(t *testing.T) {
	const base = "tests/e2e/tests/galley/testdata"

	cases := []struct {
		filename string
		valid    bool
	}{
		// TODO - APA adapter validation disabled because mixer template's attribute
		// manifest is not plumbed through to mixer validation code yet.
		//
		// {"config-v1alpha2-kubernetes-invalid", false},
		// {"config-v1alpha2-kubernetes-valid", true},

		// {"config-v1alpha2-attributemanifest-invalid", false},
		// {"config-v1alpha2-attributemanifest-valid", true},

		{"config-v1alpha2-rule-invalid", false},
		{"config-v1alpha2-rule-valid", true},

		{"config-v1alpha2-HTTPAPISpec-invalid", false},
		{"config-v1alpha2-HTTPAPISpec-valid", true},
		{"config-v1alpha2-HTTPAPISpecBinding-invalid", false},
		{"config-v1alpha2-HTTPAPISpecBinding-valid", true},
		{"config-v1alpha2-QuotaSpec-invalid", false},
		{"config-v1alpha2-QuotaSpec-valid", true},
		{"config-v1alpha2-QuotaSpecBinding-invalid", false},
		{"config-v1alpha2-QuotaSpecBinding-valid", true},

		{"rbac-v1alpha1-ServiceRole-invalid", false},
		{"rbac-v1alpha1-ServiceRole-valid", true},
		{"rbac-v1alpha1-ServiceRoleBinding-invalid", false},
		{"rbac-v1alpha1-ServiceRoleBinding-valid", true},

		{"authentication-v1alpha1-Policy-invalid", false},
		{"authentication-v1alpha1-Policy-valid", true},

		{"networking-v1alpha3-DestinationRule-invalid", false},
		{"networking-v1alpha3-DestinationRule-valid", true},
		{"networking-v1alpha3-ServiceEntry-invalid", false},
		{"networking-v1alpha3-ServiceEntry-valid", true},
		{"networking-v1alpha3-Sidecar-invalid", false},
		{"networking-v1alpha3-Sidecar-valid", true},
		{"networking-v1alpha3-VirtualService-invalid", false},
		{"networking-v1alpha3-VirtualService-valid", true},
		{"networking-v1alpha3-Gateway-invalid", false},
		{"networking-v1alpha3-Gateway-valid", true},
	}

	denied := func(err error) bool {
		if err == nil {
			return false
		}
		return strings.Contains(err.Error(), "denied the request")
	}

	t.Log("Checking for istio-galley validatingwebhookconfiguration ...")
	timeout := time.Now().Add(waitForValidatingWebhookConfigTimeout)
	for {
		if time.Now().After(timeout) {
			t.Fatal("Timeout waiting for istio-galley validatingwebhookconfiguration to be created")
		}
		if util.ValidatingWebhookConfigurationExists("istio-galley", tc.Kube.KubeConfig) {
			break
		}
		t.Log("istio-galley validatingwebhookconfiguration not ready")
		time.Sleep(waitForValidatingWebhookConfigCheckFreq)
	}
	t.Log("Found istio-galley validatingwebhookconfiguration. Proceeding with test.")

	for i := range cases {
		c := cases[i]
		t.Run(fmt.Sprintf("[%d] %s", i, c.filename), func(t *testing.T) {
			t.Parallel()
			filename := util.GetResourcePath(filepath.Join(base, c.filename+"."+yamlExtension))

			// `invalid` tests will PASS if file doesn't exist.
			if _, err := os.Stat(filename); os.IsNotExist(err) {
				t.Fatalf("%v does not exist", filename)
			}

			err := applyGalleyConfig(filename)
			switch {
			case err != nil && c.valid:
				if denied(err) {
					t.Fatalf("got unexpected for valid config: %v", err)
				} else {
					t.Fatalf("got unexpected unknown error for valid config: %v", err)
				}
			case err == nil && !c.valid:
				t.Fatalf("got unexpected success for invalid config")
			case err != nil && !c.valid:
				if !denied(err) {
					t.Fatalf("config request denied for wrong reason: %v", err)
				}
			}
		})
	}
}
