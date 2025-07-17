//go:build integ
// +build integ

// Copyright Istio Authors
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

package helm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/helm"
	"istio.io/istio/tests/util/sanitycheck"
)

// TestDefaultInstall tests Istio installation using Helm with default options
func TestDefaultInstall(t *testing.T) {
	values := map[string]interface{}{
		"global": map[string]interface{}{},
	}
	framework.
		NewTest(t).
		Run(setupInstallation(values, false, DefaultNamespaceConfig, ""))
}

// TestAmbientInstall tests Istio ambient profile installation using Helm
func TestAmbientInstall(t *testing.T) {
	valuesAmbient := map[string]interface{}{
		"profile": "ambient",
	}
	framework.
		NewTest(t).
		Run(setupInstallation(valuesAmbient, true, DefaultNamespaceConfig, ""))
}

func TestAmbientInstallMultiNamespace(t *testing.T) {
	nsConfig := NewNamespaceConfig(
		types.NamespacedName{
			Name: CniReleaseName, Namespace: "istio-cni",
		},
		types.NamespacedName{
			Name: ZtunnelReleaseName, Namespace: "ztunnel",
		},
		types.NamespacedName{
			Name: IstiodReleaseName, Namespace: "istiod",
		},
		types.NamespacedName{
			Name: IngressReleaseName, Namespace: "ingress-release",
		})
	// Setup our profile override. Ideally we could just add `trustedZtunnelNamespace`, but Istiod cannot currently be deployed
	// in another namespace without `global.istioNamespace` set.
	profileValues := map[string]interface{}{
		"global": map[string]interface{}{
			"istioNamespace": "istiod",
		},
		"profile": "ambient",
		"pilot": map[string]interface{}{
			"trustedZtunnelNamespace": "ztunnel",
		},
	}
	framework.
		NewTest(t).
		Run(setupInstallation(profileValues, true, nsConfig, ""))
}

// TestReleaseChannels tests that non-stable CRDs and fields get blocked
// by the default ValidatingAdmissionPolicy
func TestReleaseChannels(t *testing.T) {
	valuesProfileStable := map[string]interface{}{
		"profile": "stable",
	}

	framework.
		NewTest(t).
		RequireKubernetesMinorVersion(30).
		Run(setupInstallationWithCustomCheck(valuesProfileStable, false, DefaultNamespaceConfig, func(t framework.TestContext) {
			// Try to apply an EnvoyFilter (it should be rejected)
			expectedErrorPrefix := `%s "sample" is forbidden: ValidatingAdmissionPolicy 'stable-channel-default-policy.istio.io' ` +
				`with binding 'stable-channel-default-policy-binding.istio.io' denied request`
			err := t.ConfigIstio().Eval("default", nil, sampleEnvoyFilter).Apply()
			if err == nil {
				t.Errorf("Did not receive an error while applying sample EnvoyFilter with stable admission policy")
			} else {
				msg := fmt.Sprintf(expectedErrorPrefix, "envoyfilters.networking.istio.io")
				if !strings.Contains(err.Error(), msg) {
					t.Errorf("Expected error %q to contain %q", err.Error(), msg)
				}
			}

			// Now test field-level blocks with Telemetry
			err = t.ConfigIstio().Eval("default", nil, extendedTelemetry).Apply()
			if err == nil {
				t.Error("Did not receive an error while applying extended Telemetry resource with stable admission policy")
			} else {
				msg := fmt.Sprintf(expectedErrorPrefix, "telemetries.telemetry.istio.io")
				if !strings.Contains(err.Error(), msg) {
					t.Errorf("Expected error %q to contain %q", err.Error(), msg)
				}
			}
		}, ""))
}

// TestRevisionedReleaseChannels tests that non-stable CRDs and fields get blocked
// by the revisioned ValidatingAdmissionPolicy
func TestRevisionedReleaseChannels(t *testing.T) {
	valuesRevisioneRelease := map[string]interface{}{
		"profile":         "stable",
		"revision":        "1-x",
		"defaultRevision": "",
	}
	revision := "1-x"
	framework.
		NewTest(t).
		RequireKubernetesMinorVersion(30).
		Run(setupInstallationWithCustomCheck(valuesRevisioneRelease, false, DefaultNamespaceConfig, func(t framework.TestContext) {
			// Try to apply an EnvoyFilter (it should be rejected)
			expectedErrorPrefix := `%s "sample" is forbidden: ValidatingAdmissionPolicy 'stable-channel-policy-1-x-istio-system.istio.io' ` +
				`with binding 'stable-channel-policy-binding-1-x-istio-system.istio.io' denied request`
			err := t.ConfigIstio().Eval("default", nil, fmt.Sprintf(revisionedSampleEnvoyFilter, revision)).Apply()
			if err == nil {
				t.Errorf("Did not receive an error while applying sample EnvoyFilter with stable admission policy")
			} else {
				msg := fmt.Sprintf(expectedErrorPrefix, "envoyfilters.networking.istio.io")
				if !strings.Contains(err.Error(), msg) {
					t.Errorf("Expected error %q to contain %q", err.Error(), msg)
				}
			}

			// Now test field-level blocks with Telemetry
			err = t.ConfigIstio().Eval("default", nil, fmt.Sprintf(revisionedExtendedTelemetry, revision)).Apply()
			if err == nil {
				t.Error("Did not receive an error while applying extended Telemetry resource with stable admission policy")
			} else {
				msg := fmt.Sprintf(expectedErrorPrefix, "telemetries.telemetry.istio.io")
				if !strings.Contains(err.Error(), msg) {
					t.Errorf("Expected error %q to contain %q", err.Error(), msg)
				}
			}
		}, revision))
}

func TestNativeNftablesInstall(t *testing.T) {
	values := map[string]interface{}{
		"global": map[string]interface{}{
			"nativeNftables": true,
		},
	}
	framework.
		NewTest(t).
		Run(setupInstallation(values, false, DefaultNamespaceConfig, ""))
}

//nolint: unparam
func setupInstallation(values map[string]interface{}, isAmbient bool, config NamespaceConfig, revision string) func(t framework.TestContext) {
	return baseSetup(values, isAmbient, config, func(t framework.TestContext) {
		sanitycheck.RunTrafficTest(t, isAmbient)
	}, revision)
}

func setupInstallationWithCustomCheck(values map[string]interface{}, isAmbient bool, config NamespaceConfig,
	check func(t framework.TestContext), revision string,
) func(t framework.TestContext) {
	return baseSetup(values, isAmbient, config, check, revision)
}

func baseSetup(values map[string]interface{}, isAmbient bool, config NamespaceConfig,
	check func(t framework.TestContext), revision string,
) func(t framework.TestContext) {
	return func(t framework.TestContext) {
		workDir, err := t.CreateTmpDirectory("helm-install-test")
		if err != nil {
			t.Fatal("failed to create test directory")
		}
		cs := t.Clusters().Default().(*kubecluster.Cluster)
		h := helm.New(cs.Filename())
		s := t.Settings()

		// Replace the default values with the provided values
		// Check first if global exists. If not, create it
		if _, ok := values["global"]; !ok {
			values["global"] = map[string]interface{}{}
		}
		values["global"].(map[string]interface{})["tag"] = s.Image.Tag
		values["global"].(map[string]interface{})["hub"] = s.Image.Hub
		values["global"].(map[string]interface{})["variant"] = s.Image.Variant

		// Handle Openshift platform override if set
		if t.Settings().OpenShift {
			values["global"].(map[string]interface{})["platform"] = "openshift"
			// TODO: do FLATTEN_GLOBALS_REPLACEMENT to avoid this set
			values["platform"] = "openshift"
		} else {
			values["global"].(map[string]interface{})["platform"] = ""
		}

		overrideValues, err := yaml.Marshal(values)
		if err != nil {
			t.Fatalf("failed to marshal override values to YAML: %v", err)
		}

		overrideValuesFile := filepath.Join(workDir, "values.yaml")
		if err := os.WriteFile(overrideValuesFile, overrideValues, os.ModePerm); err != nil {
			t.Fatalf("failed to write iop cr file: %v", err)
		}
		t.Cleanup(func() {
			if !t.Failed() {
				return
			}
			if t.Settings().CIMode {
				for _, ns := range config.AllNamespaces() {
					namespace.Dump(t, ns)
				}
			}
		})

		InstallIstio(t, cs, h, overrideValuesFile, "", true, isAmbient, config)

		VerifyInstallation(t, cs, config, true, isAmbient, revision)
		verifyValidation(t, revision)

		check(t)
		t.Cleanup(func() {
			DeleteIstio(t, h, cs, config, isAmbient)
		})
	}
}
