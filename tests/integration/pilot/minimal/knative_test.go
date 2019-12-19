// Copyright 2019 Istio Authors
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

package minimal

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/core/image"
	"istio.io/istio/pkg/test/util/retry"
)

func TestKnative(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
				Prefix: "knative",
				Inject: true,
			})
			if err := env.Accessor.Apply("knative-serving", "testdata/knative-crds.yaml"); err != nil {
				t.Fatal(err)
			}
			if err := env.Accessor.Apply("knative-serving", "testdata/knative-serving.yaml"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if !ctx.Settings().NoCleanup {
					if err := env.Accessor.Delete("knative-serving", "testdata/knative-serving.yaml"); err != nil {
						t.Fatal(err)
					}
				}
			}()
			settings, err := image.SettingsFromCommandLine()
			if err != nil {
				t.Fatal(err)
			}
			service := `
apiVersion: serving.knative.dev/v1alpha1
kind: Service
metadata:
  name: knative-service
spec:
  runLatest:
    configuration:
      revisionTemplate:
        spec:
          container:
            image: {{ .Hub }}/app:{{ .Tag }}
`
			tmpl, err := template.New("Config").Parse(service)
			if err != nil {
				t.Errorf("failed to create template: %v", err)
			}
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, map[string]string{
				"Hub": settings.Hub,
				"Tag": settings.Tag,
			}); err != nil {
				t.Errorf("failed to create template: %v", err)
			}
			if _, err := env.Accessor.ApplyContents(ns.Name(), buf.String()); err != nil {
				t.Fatal(err)
			}

			defer func() {
				// Clean up route first; if we delete knative namespace first it may hang
				if !ctx.Settings().NoCleanup {
					if err := env.Accessor.DeleteContents(ns.Name(), buf.String()); err != nil {
						t.Fatal(err)
					}
				}
			}()

			gvr := schema.GroupVersionResource{
				Group:    "serving.knative.dev",
				Version:  "v1",
				Resource: "routes",
			}

			retry.UntilSuccessOrFail(t, func() error {
				u, err := env.GetUnstructured(gvr, ns.Name(), "knative-service")
				if err != nil {
					return fmt.Errorf("couldn't get route: %v", err)
				}
				conditions, f, err := unstructured.NestedSlice(u.Object, "status", "conditions")

				if !f || err != nil {
					return fmt.Errorf("couldn't get route conditions: error=%v, found=%v", err, f)
				}
				for _, cond := range conditions {
					c := cond.(map[string]interface{})
					if c["type"] == "Ready" {
						if c["status"] != "True" {
							return fmt.Errorf("rotue not ready, status: %+v", c)
						} else {
							t.Logf("route is ready")
							return nil
						}
					}
				}
				return nil
			})
			u, err := env.GetUnstructured(gvr, ns.Name(), "knative-service")
			if err != nil {
				t.Fatalf("couldn't get route: %v", err)
			}
			host, _, err := unstructured.NestedString(u.Object, "status", "url")
			if err != nil {
				t.Fatalf("couldn't get route host: %v", err)
			}

			if err := retry.UntilSuccess(func() error {
				resp, err := ingr.Call(ingress.CallOptions{
					Host:     strings.TrimPrefix(host, "http://"),
					Path:     "/",
					CallType: ingress.PlainText,
					Address:  ingr.HTTPAddress(),
				})
				if err != nil {
					return err
				}
				if resp.Code != 200 {
					return fmt.Errorf("got invalid response code %v: %v", resp.Code, resp.Body)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
}
