//go:build integ

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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/crd"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/helm"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/util/sanitycheck"
)

const numericNamespaceInstallEnvVar = "ISTIO_TEST_HELM_NUMERIC_NAMESPACE"

// TestDefaultInstall tests Istio installation using Helm with default options
func TestDefaultInstall(t *testing.T) {
	values := map[string]interface{}{
		"global": map[string]interface{}{},
	}
	framework.
		NewTest(t).
		Run(setupInstallation(values, false, DefaultNamespaceConfig, ""))
}

func TestRevisionedInstall(t *testing.T) {
	values := map[string]interface{}{
		"global":          map[string]interface{}{},
		"defaultRevision": "testrev",
		"revision":        "testrev",
	}
	revision := "testrev"
	framework.
		NewTest(t).
		Run(baseSetup(values, false, DefaultNamespaceConfig, func(t framework.TestContext) {
			// Install gateway API CRDs
			crd.DeployGatewayAPIOrSkip(t)
			// Verify we can create a Gateway successfully
			sampleGateway := `
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: sample
  namespace: default
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    protocol: HTTP
    port: 80
`
			t.ConfigIstio().Eval("default", nil, fmt.Sprint(sampleGateway)).ApplyOrFail(t)
			selector := klabels.NewSelector()
			req, _ := klabels.NewRequirement(label.IoK8sNetworkingGatewayGatewayName.Name, selection.Equals, []string{"sample"})
			selector.Add(*req)
			VerifyPodReady(t, t.Clusters().Default(), "default", selector.String())
			if !t.Settings().NoCleanup {
				t.ConfigIstio().Eval("default", nil, fmt.Sprint(sampleGateway)).DeleteOrFail(t)
			}
		}, revision))
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

// TestNumericNamespaceInstall verifies Helm install when the control-plane namespace is numeric-only.
// Such names must remain strings in rendered manifests (e.g. YAML fields that would otherwise parse as numbers).
//
// This test is opt-in only. Set ISTIO_TEST_HELM_NUMERIC_NAMESPACE=1 to run it, as conflicts with other tests due to CRD/CR owner references.
func TestNumericNamespaceInstall(t *testing.T) {
	if os.Getenv(numericNamespaceInstallEnvVar) != "1" {
		t.Skipf("Skipping TestNumericNamespaceInstall; set %s=1 to run", numericNamespaceInstallEnvVar)
	}

	numericNS := "123456"
	nsConfig := NewNamespaceConfig(
		types.NamespacedName{Name: BaseReleaseName, Namespace: numericNS},
		types.NamespacedName{Name: IstiodReleaseName, Namespace: numericNS},
		types.NamespacedName{Name: IngressReleaseName, Namespace: numericNS},
	)
	values := map[string]interface{}{
		"global": map[string]interface{}{
			"istioNamespace": numericNS,
		},
		// Gateway injection sets CA_ADDR from global.istioNamespace, but xDS uses
		// proxyConfig.DiscoveryAddress which defaults to istiod.istio-system.svc when
		// PROXY_CONFIG is empty. Set discoveryAddress explicitly for non-istio-system installs.
		"podAnnotations": map[string]interface{}{
			"proxy.istio.io/config": fmt.Sprintf(`{"discoveryAddress":"istiod.%s.svc:15012"}`, numericNS),
		},
	}
	framework.
		NewTest(t).
		Run(setupInstallation(values, false, nsConfig, ""))
}

// TestOwnedCNIConfigInstall verifies the istio-cni creates an owned config and removes it on uninstall.
func TestOwnedCNIConfigInstall(t *testing.T) {
	valuesAmbient := map[string]interface{}{
		"profile": "ambient",
	}
	framework.
		NewTest(t).
		Run(func(t framework.TestContext) {
			if t.Settings().OpenShift {
				t.Skip("Skipping TestOwnedCNIConfigInstall, requires a chained CNI")
			}
			setupInstallationWithCustomCheck(valuesAmbient, true, DefaultNamespaceConfig, func(t framework.TestContext) {
				cniNs := DefaultNamespaceConfig.Get(CniReleaseName)
				cs := t.Clusters().Default().(*kubecluster.Cluster)
				h := helm.New(cs.Filename())

				// setup debug pod
				cniLabel := "k8s-app=istio-cni-node"
				var debugPod *corev1.Pod
				nodeC := cs.Kube().CoreV1().Nodes()
				if nodes, err := nodeC.List(context.TODO(), metav1.ListOptions{}); err != nil {
					t.Fatalf("failed to list cluster nodes: %v", err)
				} else {
					node := nodes.Items[0].Name
					listCNIConfigOnHostCmd := fmt.Sprintf("kubectl debug node/%s -n %s --image=busybox -- sleep 120", node, cniNs)
					out, err := shell.Execute(true, listCNIConfigOnHostCmd)
					if err != nil {
						t.Fatalf("failed to list CNI config from node %v: %v", node, err)
					}
					for s := range strings.FieldsSeq(out) {
						if strings.HasPrefix(s, "node-debugger") && strings.Contains(s, node) {
							coreC := cs.Kube().CoreV1().Pods(cniNs)
							if debugPod, err = coreC.Get(context.TODO(), s, metav1.GetOptions{}); err != nil {
								t.Fatalf("failed to get debug pod %s: %v", s, err)
							}
							t.Cleanup(func() {
								_ = cs.Kube().CoreV1().Pods(debugPod.Namespace).Delete(context.TODO(), debugPod.Name, metav1.DeleteOptions{})
							})
							break
						}
					}
					if debugPod == nil {
						t.Fatalf("debug Pod not found: %s", out)
					}
				}

				expectCNIConfigs := func(expected []string) error {
					stdout, _, err := cs.PodExec(debugPod.Name, debugPod.Namespace, "", "ls /host/etc/cni/net.d/")
					if err != nil {
						return err
					}
					files := strings.Fields(stdout)
					if slices.Equal(expected, files) {
						return nil
					}
					return fmt.Errorf("config mistmatch, found %s", stdout)
				}

				getIstioPlugin := func(filename string) (map[string]any, error) {
					stdout, _, err := cs.PodExec(debugPod.Name, debugPod.Namespace, "", "cat /host/etc/cni/net.d/"+filename)
					if err != nil {
						return nil, err
					}
					var cniConfigMap map[string]any
					if err = json.Unmarshal([]byte(stdout), &cniConfigMap); err != nil {
						return nil, fmt.Errorf("unmarshal failed for %s: %w", filename, err)
					}

					plugins, err := util.GetPlugins(cniConfigMap)
					if err != nil {
						return nil, fmt.Errorf("no plugins: %v", err)
					}

					for _, rawPlugin := range plugins {
						plugin, err := util.GetPlugin(rawPlugin)
						if err != nil {
							return nil, fmt.Errorf("bad CNI plugin: %v", err)
						}
						if plugin["type"] == "istio-cni" {
							return plugin, nil
						}
					}
					return nil, nil
				}
				hasConfigValues := func(plugin map[string]any, expected map[string]any) error {
					for k, v := range expected {
						if plugin[k] != v {
							return fmt.Errorf("incorrect plugin value for %s: %#v", k, plugin)
						}
					}
					return nil
				}

				workDir, err := t.CreateTmpDirectory("cniconfig-test")
				if err != nil {
					t.Fatal("failed to create test directory")
				}

				cniChartPath := filepath.Join(ManifestsChartPath, CniChartsDir)
				upgradeChart := func(values map[string]any, args ...string) {
					overrideValues, err := yaml.Marshal(values)
					if err != nil {
						t.Fatalf("failed to marshal override values to YAML: %v", err)
					}

					overrideValuesFile := filepath.Join(workDir, "values.yaml")
					if err := os.WriteFile(overrideValuesFile, overrideValues, os.ModePerm); err != nil {
						t.Fatalf("failed to write values file: %v", err)
					}
					if err := h.UpgradeChart(CniReleaseName, cniChartPath, cniNs, overrideValuesFile, Timeout, args...); err != nil {
						t.Fatalf("failed to upgrade istio %s chart", CniReleaseName)
					}
					VerifyPodReady(t, cs, cniNs, cniLabel)
				}

				settings := t.Settings()
				cniValues := map[string]any{
					"global": map[string]any{
						"tag":     settings.Image.Tag,
						"hub":     settings.Image.Hub,
						"variant": settings.Image.Variant,
					},
				}
				t.NewSubTest("initial install").Run(func(t framework.TestContext) {
					// primary CNIConfig only
					retry.UntilSuccessOrFail(t, func() error {
						return expectCNIConfigs([]string{"10-kindnet.conflist"})
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))

					retry.UntilSuccessOrFail(t, func() error {
						plugin, err := getIstioPlugin("10-kindnet.conflist")
						if err != nil {
							return err
						}
						return hasConfigValues(plugin, map[string]any{
							"ambient_enabled":                true,
							"enable_ambient_detection_retry": false,
						})
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))

					sanitycheck.RunTrafficTest(t, true)
				})

				t.NewSubTest("enable detection retry").Run(func(t framework.TestContext) {
					// enable retry
					upgradeChart(maps.MergeCopy(cniValues, map[string]any{
						"profile": "ambient",
						"ambient": map[string]any{
							"enableAmbientDetectionRetry": true,
						},
					}))

					retry.UntilSuccessOrFail(t, func() error {
						return expectCNIConfigs([]string{"10-kindnet.conflist"})
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))

					retry.UntilSuccessOrFail(t, func() error {
						plugin, err := getIstioPlugin("10-kindnet.conflist")
						if err != nil {
							return err
						}
						return hasConfigValues(plugin, map[string]any{
							"ambient_enabled":                true,
							"enable_ambient_detection_retry": true,
						})
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))

					sanitycheck.RunTrafficTest(t, true)
				})

				t.NewSubTest("enable istio owned config").Run(func(t framework.TestContext) {
					// enable istioOWnedCNIConfig
					upgradeChart(maps.MergeCopy(cniValues, map[string]any{
						"profile":             "ambient",
						"istioOwnedCNIConfig": true,
					}))

					// verify the owned cni is present
					retry.UntilSuccessOrFail(t, func() error {
						return expectCNIConfigs([]string{"02-istio-cni.conflist", "10-kindnet.conflist"})
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))

					retry.UntilSuccessOrFail(t, func() error {
						plugin, err := getIstioPlugin("02-istio-cni.conflist")
						if err != nil {
							return err
						}
						return hasConfigValues(plugin, map[string]any{
							"ambient_enabled":                true,
							"enable_ambient_detection_retry": false,
						})
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))

					retry.UntilSuccessOrFail(t, func() error {
						plugin, err := getIstioPlugin("10-kindnet.conflist")
						if err != nil {
							return err
						}
						if plugin != nil {
							return fmt.Errorf("istio-cni is present")
						}
						return nil
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))

					sanitycheck.RunTrafficTest(t, true)
				})

				t.NewSubTest("uninstall").Run(func(t framework.TestContext) {
					// uninstall istio-cni
					if err := h.DeleteChart(CniReleaseName, cniNs); err == nil {
					} else {
						t.Errorf("failed to delete %s release: %v", CniReleaseName, err)
					}

					// verify the owned cni is removed
					retry.UntilSuccessOrFail(t, func() error {
						return expectCNIConfigs([]string{"10-kindnet.conflist"})
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))

					retry.UntilSuccessOrFail(t, func() error {
						plugin, err := getIstioPlugin("10-kindnet.conflist")
						if err != nil {
							return err
						}
						if plugin != nil {
							return fmt.Errorf("istio-cni should not nil: %+v", plugin)
						}
						return nil
					}, retry.Timeout(10*time.Second), retry.Delay(RetryDelay))
				})

				// satisfy the cleanup
				upgradeChart(maps.MergeCopy(cniValues, map[string]any{
					"profile": "ambient",
				}), "--install")
			}, "")(t)
		})
}

// nolint: unparam
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

		t.Cleanup(func() {
			if !t.Settings().NoCleanup {
				DeleteIstio(t, h, cs, config, isAmbient)
			}
		})

		check(t)
	}
}
