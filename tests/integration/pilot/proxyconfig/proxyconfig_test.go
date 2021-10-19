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

package proxyconfig

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
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

func TestProxyConfig(t *testing.T) {
	framework.NewTest(t).
		Features("usability.observability.proxy-config").
		RequireIstioVersion("1.13").
		Run(func(ctx framework.TestContext) {
			cases := []struct {
				name string
				// namespace, labels, and annotations for the echo instance
				namespace    string
				pcAnnotation string
				// proxyconfig resources to apply
				config string
				// expected environment variables post-injection
				expected map[string]string
			}{
				{
					"meshconfig.defaultConfig values are set",
					"a",
					"",
					"",
					map[string]string{
						"A": "1",
						"B": "2",
					},
				},
				{
					"proxy config values override meshconfig.defaultConfig",
					"b",
					"",
					newProxyConfig("namespace-b", nil, map[string]string{
						"A": "3",
					}),
					map[string]string{
						"A": "3",
						"B": "2",
					},
				},
				{
					"pod annotation overrides namespace level proxy config CR",
					"c",
					"{ \"proxyMetadata\": {\"A\": \"5\"} }",
					newProxyConfig("namespace-c", nil, map[string]string{
						"A": "4",
					}),
					map[string]string{
						"A": "5",
					},
				},
			}

			for _, tc := range cases {
				ctx.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					ns := namespace.NewOrFail(t, t, namespace.Config{
						Prefix: tc.namespace,
						Inject: true,
					})
					t.Config(t.Clusters()...).ApplyYAMLOrFail(t, ns.Name(), tc.config)
					defer t.Config(t.Clusters()...).DeleteYAMLOrFail(t, ns.Name(), tc.config)

					echoConfig := echo.Config{
						Namespace: ns,
					}
					if tc.pcAnnotation != "" {
						echoConfig.Subsets = []echo.SubsetConfig{
							{
								Annotations: map[echo.Annotation]*echo.AnnotationValue{
									echo.SidecarConfig: {
										Value: tc.pcAnnotation,
									},
								},
							},
						}
					}

					instances := echoboot.NewBuilder(ctx, t.Clusters()...).WithConfig(echoConfig).BuildOrFail(t)
					checkInjectedValues(t, instances, tc.expected)
				})
			}
		})
}

func checkInjectedValues(t framework.TestContext, instances echo.Instances, values map[string]string) {
	t.Helper()
	for _, i := range instances {
		i := i
		retry.UntilSuccessOrFail(t, func() error {
			// to avoid doing a sleep to allow time for the ProxyConfig CR to propagate, we
			// can just retrigger injection on every retry.
			err := i.Restart()
			if err != nil {
				return fmt.Errorf("failed to restart echo instance: %v", err)
			}
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

func newProxyConfig(name string, selector, values map[string]string) string {
	tpl := `
apiVersion: networking.istio.io/v1beta1
kind: ProxyConfig
metadata:
  name: {{ .Name }}
spec:
{{- if .Selector }}
  selector:
{{- range $k, $v := .Annotations }}
    $k: $v
{{- end }}
{{- end }}
  environmentVariables:
{{- range $k, $v := .Values }}
    {{ $k }}: "{{ $v }}"
{{- end }}
`
	return tmpl.MustEvaluate(tpl, struct {
		Name     string
		Selector map[string]string
		Values   map[string]string
	}{
		Name:     name,
		Selector: selector,
		Values:   values,
	})
}
