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

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const yamlExtension = "yaml"

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

func deleteGalleyConfig(ruleName string) error {
	return doGalleyConfig(ruleName, util.KubeDeleteContents)
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

func TestValidation(t *testing.T) {
	const base = "tests/e2e/tests/galley/testdata"

	cases := []struct {
		filename string
		valid    bool
	}{
		{"config-v1alpha2-apikey-invalid", false},
		{"config-v1alpha2-apikey-valid", true},
		{"config-v1alpha2-authorization-invalid", false},
		{"config-v1alpha2-authorization-valid", true},
		{"config-v1alpha2-checknothing-invalid", false},
		{"config-v1alpha2-checknothing-valid", true},
		{"config-v1alpha2-circonus-invalid", false},
		{"config-v1alpha2-circonus-valid", true},
		{"config-v1alpha2-denier-invalid", false},
		{"config-v1alpha2-denier-valid", true},
		{"config-v1alpha2-fluentd-invalid", false},
		{"config-v1alpha2-fluentd-valid", true},

		// TODO - APA adapter validation disabled because mixer template's attribute
		// manifest is not plumbed through to mixer validation code yet.
		//
		// {"config-v1alpha2-kubernetes-invalid", false},
		// {"config-v1alpha2-kubernetes-valid", true},

		{"config-v1alpha2-kubernetesenv-invalid", false},
		{"config-v1alpha2-kubernetesenv-valid", true},
		{"config-v1alpha2-listchecker-invalid", false},
		{"config-v1alpha2-listchecker-valid", true},
		{"config-v1alpha2-listentry-invalid", false},
		{"config-v1alpha2-listentry-valid", true},
		{"config-v1alpha2-logentry-invalid", false},
		{"config-v1alpha2-logentry-valid", true},
		{"config-v1alpha2-memquota-invalid", false},
		{"config-v1alpha2-memquota-valid", true},
		{"config-v1alpha2-metric-invalid", false},
		{"config-v1alpha2-metric-valid", true},
		{"config-v1alpha2-noop-invalid", false},
		{"config-v1alpha2-noop-valid", true},
		{"config-v1alpha2-opa-invalid", false},
		{"config-v1alpha2-opa-valid", true},
		{"config-v1alpha2-prometheus-invalid", false},
		{"config-v1alpha2-prometheus-valid", true},
		{"config-v1alpha2-quota-invalid", false},
		{"config-v1alpha2-quota-valid", true},
		{"config-v1alpha2-rbac-invalid", false},
		{"config-v1alpha2-rbac-valid", true},
		{"config-v1alpha2-reportnothing-invalid", false},
		{"config-v1alpha2-reportnothing-valid", true},
		{"config-v1alpha2-servicecontrol-invalid", false},
		{"config-v1alpha2-servicecontrol-valid", true},
		{"config-v1alpha2-servicecontrolreport-invalid", false},
		{"config-v1alpha2-servicecontrolreport-valid", true},
		{"config-v1alpha2-solarwinds-invalid", false},
		{"config-v1alpha2-solarwinds-valid", true},
		{"config-v1alpha2-stackdriver-invalid", false},
		{"config-v1alpha2-stackdriver-valid", true},
		{"config-v1alpha2-statsd-invalid", false},
		{"config-v1alpha2-statsd-valid", true},
		{"config-v1alpha2-stdio-invalid", false},
		{"config-v1alpha2-stdio-valid", true},
		{"config-v1alpha2-tracespan-invalid", false},
		{"config-v1alpha2-tracespan-valid", true},

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
		// TODO(https://github.com/istio/istio/issues/6689)
		// ServiceEntry is temporarily disabled. Verify that invalid
		// configuration is not rejected by galley.
		{"networking-v1alpha3-ServiceEntry-invalid", true},
		{"networking-v1alpha3-ServiceEntry-valid", true},
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
