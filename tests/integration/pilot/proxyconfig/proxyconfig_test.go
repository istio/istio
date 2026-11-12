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

package proxyconfig

import (
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
)

var i istio.Instance

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Skip("used for feature development, no need to run in CI").
		Label(label.Full).
		Label(label.CustomSetup).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  meshConfig:
    defaultConfig:
      proxyMetadata:
        A: "1"
        B: "2"
      `
		})).
		Run()
}

type proxyConfigInstance struct {
	namespace string
	config    string
}

func TestProxyConfig(t *testing.T) {
	framework.NewTest(t).
		RequireIstioVersion("1.13").
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, namespace.Config{
				Prefix: "pc-test",
				Inject: true,
			})
			cases := []struct {
				name string
				// namespace, labels, and annotations for the echo instance
				pcAnnotation string
				// service, echo service to use for this subtest
				service string
				// proxyconfig resources to apply
				configs []proxyConfigInstance
				// expected environment variables post-injection
				expected map[string]string
			}{
				{
					"default config maintained",
					"",
					"",
					[]proxyConfigInstance{},
					map[string]string{
						"A": "1",
						"B": "2",
					},
				},
				{
					"global takes precedence over default config",
					"",
					"",
					[]proxyConfigInstance{
						newProxyConfig("global", "istio-system", nil, map[string]string{
							"A": "3",
						}),
					},
					map[string]string{
						"A": "3",
						"B": "2",
					},
				},
				{
					"pod annotation takes precedence over namespace",
					"{ \"proxyMetadata\": {\"A\": \"5\"} }",
					"",
					[]proxyConfigInstance{
						newProxyConfig("namespace-scoped", ns.Name(), nil, map[string]string{
							"A": "4",
						}),
					},
					map[string]string{
						"A": "5",
					},
				},
				{
					"workload selector takes precedence over namespace",
					"",
					"matcher",
					[]proxyConfigInstance{
						newProxyConfig("namespace-scoped", ns.Name(), nil, map[string]string{
							"A": "6",
						}),
						newProxyConfig("workload-selector", ns.Name(), map[string]string{
							"app": "matcher",
						}, map[string]string{
							"A": "5",
						}),
					},
					map[string]string{
						"A": "5",
					},
				},
			}

			for i, tc := range cases {
				ctx.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					applyProxyConfigs(t, tc.configs)
					defer deleteProxyConfigs(t, tc.configs)

					svc := fmt.Sprintf("echo-%d", i)
					if tc.service != "" {
						svc = tc.service
					}
					echoConfig := echo.Config{
						Namespace: ns,
						Service:   svc,
					}
					if tc.pcAnnotation != "" {
						echoConfig.Subsets = []echo.SubsetConfig{
							{
								Annotations: map[string]string{
									annotation.ProxyConfig.Name: tc.pcAnnotation,
								},
							},
						}
					}

					instances := deployment.New(ctx, t.Clusters().Configs()...).WithConfig(echoConfig).BuildOrFail(t)
					checkInjectedValues(t, instances, tc.expected)
				})
			}
		})
}

func TestProxyConfigResources(t *testing.T) {
	framework.NewTest(t).
		Run(func(ctx framework.TestContext) {
			type sizing struct {
				requestsCPU, requestsMemory, limitsCPU, limitsMemory string
			}
			cases := []struct {
				prefix string
				want   sizing
			}{
				{"pc-resources-a", sizing{"50m", "64Mi", "200m", "128Mi"}},
				{"pc-resources-b", sizing{"100m", "96Mi", "400m", "256Mi"}},
			}
			for _, tc := range cases {
				ns := namespace.NewOrFail(ctx, namespace.Config{
					Prefix: tc.prefix,
					Inject: true,
				})
				ctx.ConfigIstio().YAML(ns.Name(), fmt.Sprintf(`
apiVersion: networking.istio.io/v1beta1
kind: ProxyConfig
metadata:
  name: sizing
spec:
  resources:
    requests:
      cpu: %s
      memory: %s
    limits:
      cpu: %s
      memory: %s
`, tc.want.requestsCPU, tc.want.requestsMemory, tc.want.limitsCPU, tc.want.limitsMemory)).ApplyOrFail(ctx)

				instances := deployment.New(ctx, ctx.Clusters().Configs()...).
					WithConfig(echo.Config{Namespace: ns, Service: "sized"}).
					BuildOrFail(ctx)
				for _, inst := range instances {
					attempts := 0
					retry.UntilSuccessOrFail(ctx, func() error {
						if attempts > 0 {
							if err := inst.Restart(); err != nil {
								return fmt.Errorf("failed to restart echo instance: %v", err)
							}
						}
						attempts++
						for _, w := range inst.WorkloadsOrFail(ctx) {
							pod, err := inst.Config().Cluster.Kube().CoreV1().Pods(ns.Name()).Get(ctx.Context(), w.PodName(), metav1.GetOptions{})
							if err != nil {
								return err
							}
							got := proxyResources(pod)
							if got == nil {
								return fmt.Errorf("istio-proxy container not found in pod %s", w.PodName())
							}
							actual := sizing{
								got.Requests.Cpu().String(), got.Requests.Memory().String(),
								got.Limits.Cpu().String(), got.Limits.Memory().String(),
							}
							if actual != tc.want {
								return fmt.Errorf("pod %s: expected istio-proxy resources %+v, got %+v", w.PodName(), tc.want, actual)
							}
						}
						return nil
					}, retry.Timeout(time.Second*45))
				}
			}
		})
}

func proxyResources(pod *corev1.Pod) *corev1.ResourceRequirements {
	for _, c := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
		if c.Name == "istio-proxy" {
			return &c.Resources
		}
	}
	return nil
}

func checkInjectedValues(t framework.TestContext, instances echo.Instances, values map[string]string) {
	t.Helper()
	for _, i := range instances {
		attempts := 0
		retry.UntilSuccessOrFail(t, func() error {
			// to avoid sleeping for ProxyConfig propagation, we
			// can just re-trigger injection on every retry.
			if attempts > 0 {
				err := i.Restart()
				if err != nil {
					return fmt.Errorf("failed to restart echo instance: %v", err)
				}
			}
			attempts++
			for _, w := range i.WorkloadsOrFail(t) {
				for k, v := range values {
					// can we rely on printenv being in the container once distroless is default?
					out, _, err := i.Config().Cluster.PodExec(w.PodName(), i.Config().Namespace.Name(),
						"istio-proxy", fmt.Sprintf("printenv %s", k))
					out = strings.TrimSuffix(out, "\n")
					if err != nil {
						return fmt.Errorf("could not exec into pod: %v", err)
					}
					if out != v {
						return fmt.Errorf("expected envvar %s with value %q, got %q", k, v, out)
					}
				}
			}
			return nil
		}, retry.Timeout(time.Second*45))
	}
}

func applyProxyConfigs(ctx framework.TestContext, configs []proxyConfigInstance) {
	for _, config := range configs {
		ctx.ConfigIstio().YAML(config.namespace, config.config).ApplyOrFail(ctx)
	}
	// Propagation is handled by checkInjectedValues, which retries with echo
	// Restart() to re-trigger injection once ProxyConfig is reconciled by istiod.
}

func deleteProxyConfigs(ctx framework.TestContext, configs []proxyConfigInstance) {
	for _, config := range configs {
		ctx.ConfigIstio().YAML(config.namespace, config.config).DeleteOrFail(ctx)
	}
}

func newProxyConfig(name, ns string, selector, values map[string]string) proxyConfigInstance {
	tpl := `
apiVersion: networking.istio.io/v1beta1
kind: ProxyConfig
metadata:
  name: {{ .Name }}
spec:
{{- if .Selector }}
  selector:
    matchLabels:
{{- range $k, $v := .Selector }}
      {{ $k }}: {{ $v }}
{{- end }}
{{- end }}
  environmentVariables:
{{- range $k, $v := .Values }}
    {{ $k }}: "{{ $v }}"
{{- end }}
`
	return proxyConfigInstance{
		namespace: ns,
		config: tmpl.MustEvaluate(tpl, struct {
			Name     string
			Selector map[string]string
			Values   map[string]string
		}{
			Name:     name,
			Selector: selector,
			Values:   values,
		}),
	}
}
