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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tests/util/sanitycheck"
)

// TestDefaultInstall tests Istio installation using Helm with default options
func TestDefaultInstall(t *testing.T) {
	overrideValuesStr := `
global:
  hub: %s
  tag: %s
`
	framework.
		NewTest(t).
		Features("installation.helm.default.install").
		Run(setupInstallation(overrideValuesStr))
}

// TestDefaultInstallCustomReleaseNamespace tests the scenario where the
// Istio Helm releases are installed in a different namespace than istio-system.
// The actual control plane resources will be installed in my-istio-system.
func TestDefaultInstallCustomReleaseNamespace(t *testing.T) {
	customSystemNamespace := "my-istio-system"
	gatewayOverrideValuesStr := `
global:
  hub: %s
  tag: %s
namespace: %s
`
	overrideValuesStr := `
global:
  hub: %s
  tag: %s
  istioNamespace: %s
`
	overrideValuesMap := map[string]string{
		RepoBaseChartPath:      overrideValuesStr,
		RepoDiscoveryChartPath: overrideValuesStr,
		RepoGatewayChartPath:   gatewayOverrideValuesStr,
	}

	helmReleaseNamespace := "istio-helm-releases"

	genInjectionTemplates := func(t framework.TestContext, systemNamespace string) (map[string]sets.String, error) {
		out := map[string]sets.String{}
		for _, c := range t.Clusters().Kube() {
			out[c.Name()] = sets.New[string]()
			// TODO find a place to read revision(s) and avoid listing
			cms, err := c.Kube().CoreV1().ConfigMaps(systemNamespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return nil, err
			}

			// take the intersection of the templates available from each revision in this cluster
			intersection := sets.New[string]()
			for _, item := range cms.Items {
				if !strings.HasPrefix(item.Name, "istio-sidecar-injector") {
					continue
				}
				data, err := inject.UnmarshalConfig([]byte(item.Data["config"]))
				if err != nil {
					return nil, fmt.Errorf("failed parsing injection cm in %s: %v", c.Name(), err)
				}
				if data.RawTemplates != nil {
					t := sets.New[string]()
					for name := range data.RawTemplates {
						t.Insert(name)
					}
					// either intersection has not been set or we intersect these templates
					// with the current set.
					if intersection.IsEmpty() {
						intersection = t
					} else {
						intersection = intersection.Intersection(t)
					}
				}
			}
			for name := range intersection {
				out[c.Name()].Insert(name)
			}
		}

		return out, nil
	}

	runSanityCheck := func(t framework.TestContext, systemNamespace string) {
		scopes.Framework.Infof("running sanity test")
		var client, server echo.Instance
		testNs := namespace.NewOrFail(t, t, namespace.Config{
			Prefix:   "default",
			Revision: "",
			Inject:   true,
		})

		templates, err := genInjectionTemplates(t, systemNamespace)
		if err != nil {
			t.Fatalf("failed to locate injection templates: %v", err)
		}
		builder := deployment.New(t)

		builder.
			With(&client, echo.Config{
				Service:   "client",
				Namespace: testNs,
				Ports:     []echo.Port{},
			}).
			With(&server, echo.Config{
				Service:   "server",
				Namespace: testNs,
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						WorkloadPort: 8090,
					},
				},
			}).
			WithTemplates(templates). // deployment.New() defaults to istio-system as the systemNamespace; change that to our custom namespace here
			BuildOrFail(t)

		sanitycheck.RunTrafficTestClientServer(t, client, server)
	}

	verifyInstallation := func(ctx framework.TestContext, cs cluster.Cluster, verifyGateway bool, systemNamespace string) {
		scopes.Framework.Infof("=== verifying istio installation === ")

		VerifyValidatingWebhookConfigurations(ctx, cs, []string{
			"istiod-default-validator",
		})

		retry.UntilSuccessOrFail(ctx, func() error {
			if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, systemNamespace, "app=istiod")); err != nil {
				return fmt.Errorf("istiod pod is not ready: %v", err)
			}

			if verifyGateway {
				if _, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, systemNamespace, "app=istio-ingress")); err != nil {
					return fmt.Errorf("istio ingress gateway pod is not ready: %v", err)
				}
			}
			return nil
		}, retry.Timeout(RetryTimeOut), retry.Delay(RetryDelay))
		scopes.Framework.Infof("=== succeeded ===")
	}

	verifyValidation := func(ctx framework.TestContext, systemNamespace string) {
		ctx.Helper()
		invalidGateway := &v1alpha3.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-istio-gateway",
				Namespace: systemNamespace,
			},
			Spec: networking.Gateway{},
		}

		createOptions := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
		istioClient := ctx.Clusters().Default().Istio().NetworkingV1alpha3()
		retry.UntilOrFail(ctx, func() bool {
			_, err := istioClient.Gateways(systemNamespace).Create(context.TODO(), invalidGateway, createOptions)
			rejected := err != nil
			return rejected
		})
	}

	framework.
		NewTest(t).
		Features("installation.helm.default.install").
		Run(func(t framework.TestContext) {
			workDir, err := t.CreateTmpDirectory("helm-install-custom-release-namespace-test")
			if err != nil {
				t.Fatal("failed to create test directory")
			}
			cs := t.Clusters().Default().(*kubecluster.Cluster)
			h := helm.New(cs.Filename())
			s := t.Settings()
			for chart, values := range overrideValuesMap {
				overrideValues := fmt.Sprintf(values, s.Image.Hub, s.Image.Tag, customSystemNamespace)
				overrideValuesFile := filepath.Join(workDir, chart+"-values.yaml")
				if err := os.WriteFile(overrideValuesFile, []byte(overrideValues), os.ModePerm); err != nil {
					t.Fatalf("failed to write helm values file: %v", err)
				}
				overrideValuesMap[chart] = overrideValuesFile
			}

			t.Cleanup(func() {
				if !t.Failed() {
					return
				}
				if t.Settings().CIMode {
					namespace.Dump(t, helmReleaseNamespace)
				}
			})

			InstallIstioCustomReleaseNamespace(t, cs, h, helmReleaseNamespace, "", true, overrideValuesMap, func() {
				CreateNamespace(t, cs, helmReleaseNamespace)
				CreateNamespace(t, cs, customSystemNamespace)
			})

			verifyInstallation(t, cs, true, customSystemNamespace)
			verifyValidation(t, customSystemNamespace)

			runSanityCheck(t, customSystemNamespace)

			t.Cleanup(func() {
				scopes.Framework.Infof("cleaning up resources")
				if err := h.DeleteChart(IngressReleaseName, helmReleaseNamespace); err != nil {
					t.Errorf("failed to delete %s release: %v", IngressReleaseName, err)
				}
				if err := h.DeleteChart(IstiodReleaseName, helmReleaseNamespace); err != nil {
					t.Errorf("failed to delete %s release: %v", IstiodReleaseName, err)
				}
				if err := h.DeleteChart(BaseReleaseName, helmReleaseNamespace); err != nil {
					t.Errorf("failed to delete %s release: %v", BaseReleaseName, err)
				}
				if err := cs.Kube().CoreV1().Namespaces().Delete(context.TODO(), customSystemNamespace, metav1.DeleteOptions{}); err != nil {
					t.Errorf("failed to delete istio namespace: %v", err)
				}
				if err := kubetest.WaitForNamespaceDeletion(cs.Kube(), customSystemNamespace, retry.Timeout(RetryTimeOut)); err != nil {
					t.Errorf("waiting for istio namespace to be deleted: %v", err)
				}
				if err := cs.Kube().CoreV1().Namespaces().Delete(context.TODO(), helmReleaseNamespace, metav1.DeleteOptions{}); err != nil {
					t.Errorf("failed to delete helm release namespace %s: %v", helmReleaseNamespace, err)
				}
				if err := kubetest.WaitForNamespaceDeletion(cs.Kube(), helmReleaseNamespace, retry.Timeout(RetryTimeOut)); err != nil {
					t.Errorf("waiting for istio helm release namespace to be deleted: %v", err)
				}
			})
		})
}

// TestInstallWithFirstPartyJwt tests Istio installation using Helm
// with first-party-jwt enabled
// (TODO) remove this test when Istio no longer supports first-party-jwt
func TestInstallWithFirstPartyJwt(t *testing.T) {
	overrideValuesStr := `
global:
  hub: %s
  tag: %s
  jwtPolicy: first-party-jwt
`

	framework.
		NewTest(t).
		Features("installation.helm.firstpartyjwt.install").
		Run(func(t framework.TestContext) {
			setupInstallation(overrideValuesStr)(t)
		})
}

func setupInstallation(overrideValuesStr string) func(t framework.TestContext) {
	return func(t framework.TestContext) {
		workDir, err := t.CreateTmpDirectory("helm-install-test")
		if err != nil {
			t.Fatal("failed to create test directory")
		}
		cs := t.Clusters().Default().(*kubecluster.Cluster)
		h := helm.New(cs.Filename())
		s := t.Settings()
		overrideValues := fmt.Sprintf(overrideValuesStr, s.Image.Hub, s.Image.Tag)
		overrideValuesFile := filepath.Join(workDir, "values.yaml")
		if err := os.WriteFile(overrideValuesFile, []byte(overrideValues), os.ModePerm); err != nil {
			t.Fatalf("failed to write iop cr file: %v", err)
		}
		t.Cleanup(func() {
			if !t.Failed() {
				return
			}
			if t.Settings().CIMode {
				namespace.Dump(t, IstioNamespace)
			}
		})
		InstallIstio(t, cs, h, overrideValuesFile, "", true)

		VerifyInstallation(t, cs, true)
		verifyValidation(t)

		sanitycheck.RunTrafficTest(t, t)
		t.Cleanup(func() {
			deleteIstio(t, h, cs)
		})
	}
}
