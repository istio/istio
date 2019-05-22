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

package out_of_process_adapter_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/httpbin"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/policybackend"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	keyvalTmpl       = "../../../../mixer/test/keyval/template.yaml"
	adapterConfig    = "../../../../pkg/test/fakes/policy/policybackend.yaml"
	checknothingTmpl = "../../../../mixer/template/checknothing/template.yaml"
	statusOK         = 200
	statusTeapot     = 418
)

var (
	ist istio.Instance
)

// TestRouteDirective is to test using an out of process policy adapter to manipulate request headers and routing.
// It follows this task: https://istio.io/docs/tasks/policy-enforcement/control-headers/
func TestRouteDirective(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			httpbinNs, err := namespace.New(ctx, "httpbin-test", true)
			istioSystemNs := namespace.ClaimOrFail(t, ctx, "istio-system")

			g := galley.NewOrFail(t, ctx, galley.Config{})
			_ = mixer.NewOrFail(t, ctx, mixer.Config{Galley: g})
			_ = httpbin.DeployOrFail(t, ctx, httpbin.Config{Namespace: httpbinNs})
			be := policybackend.NewOrFail(t, ctx)

			// Get out of process adapter related config, which includes dyanmic template for keyval and checknothing,
			// and dynamic adapter config for policy backend.
			oopConfig, err := readOOPConfig()
			if err != nil {
				t.Fatalf("unable to read out of process adapter config: %v", err)
			}
			pbCfg := be.CreateConfigSnippet("policy-backend", istioSystemNs.Name(), policybackend.OutOfProcess)
			// Apply all configs needed by out of process adapter, which includes all needed dynamic templates,
			// adapter config, instances and handlers.
			g.ApplyConfigOrFail(t, istioSystemNs,
				oopConfig,
				testOOPKeyValInstace,
				pbCfg,
			)
			g.ApplyConfigOrFail(t, httpbinNs,
				httpbin.NetworkingHttpbinGateway.LoadWithNamespaceOrFail(t, httpbinNs.Name()))
			ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: ist})

			var response ingress.CallResponse

			retry.UntilSuccessOrFail(t, func() error {
				t.Logf("try visiting httpbin to get 200")
				response, err = ing.Call("/")
				if err != nil {
					return fmt.Errorf("unable to connect to httpbin: %v", err)
				}
				if response.Code != 200 {
					return fmt.Errorf("get non-ok response code %v", response.Code)
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(30*time.Second))

			// Test rule that adds header.
			t.Logf("apply rule to add 'group' header")
			g.ApplyConfigOrFail(t, istioSystemNs, testOOPKeyValGroupRule)

			// Retry visiting httpbin until gets "user-group" header
			retry.UntilSuccessOrFail(t, func() error {
				response, err = ing.CallWithHeaders("/headers", http.Header{"user": []string{"jason"}})
				if err != nil {
					return fmt.Errorf("unable to connect to httpbin: %v", err)
				}
				if response.Code != statusOK {
					return fmt.Errorf("visit returns non-ok code: %v", response.Code)
				}

				var result, headers map[string]interface{}
				if json.Unmarshal([]byte(response.Body), &result) != nil {
					return fmt.Errorf("cannot unmarshal json string %v", response.Body)
				}
				if _, ok := result["headers"]; !ok {
					return fmt.Errorf("cannot find headers in httpbin response: %v", response.Body)
				}
				headers = result["headers"].(map[string]interface{})
				for key, val := range headers {
					if strings.EqualFold(key, "user-group") && strings.EqualFold(val.(string), "admin") {
						return nil
					}
				}
				t.Logf("try visiting httpbin with 'user: jason' header, got %v want user-group header", headers)
				return fmt.Errorf("cannot find user-group header in request headers: %v", headers)
			}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))

			t.Logf("find user-group: admin header")
			g.DeleteConfigOrFail(t, istioSystemNs, testOOPKeyValGroupRule)

			// Test header editing and route directive.
			t.Logf("apply rule to edit path header")
			g.ApplyConfigOrFail(t, istioSystemNs, testOOPKeyValPathRule)
			retry.UntilSuccessOrFail(t, func() error {
				response, err = ing.CallWithHeaders("/headers", http.Header{"user": []string{"jason"}})
				if err != nil {
					return fmt.Errorf("unable to connect to httpbin: %v", err)
				}
				if response.Code != statusTeapot {
					return fmt.Errorf("httpbin does not return teapot: %v", response.Code)
				}
				t.Logf("try visiting httpbin with 'user: jason' header, got %v, want to get status %v", response.Code, statusTeapot)
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
			t.Logf("httpbin returns status 418")
			g.DeleteConfigOrFail(t, istioSystemNs, testOOPKeyValPathRule, pbCfg)
		})
}

// TestCheck tests policy check deniel with an out of process policy adapter.
func TestDenyCheck(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			gal := galley.NewOrFail(t, ctx, galley.Config{})
			mxr := mixer.NewOrFail(t, ctx, mixer.Config{
				Galley: gal,
			})
			be := policybackend.NewOrFail(t, ctx)

			ns := namespace.NewOrFail(t, ctx, "testcheck-deny-oop", false)
			istioSystemNs := namespace.ClaimOrFail(t, ctx, "istio-system")

			retry.UntilSuccessOrFail(t, func() error {
				t.Log("try call mixer check, it should pass since no deny rule applied")
				result := mxr.Check(t, map[string]interface{}{
					"context.protocol":      "http",
					"destination.name":      "someworkload",
					"destination.namespace": ns.Name(),
					"response.time":         time.Now(),
					"request.time":          time.Now(),
					"destination.service":   `svc.` + ns.Name(),
					"origin.ip":             []byte{1, 2, 3, 4},
				})

				if !result.Succeeded() {
					t.Logf("check denied with result: %v", result.Raw)
					return fmt.Errorf("check should pass: %v", result.Raw)
				}

				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(time.Second*80))

			t.Log("check could pass, apply out of process adapter config for check deny")
			// Get out of process adapter related config, which includes dyanmic template for keyval and checknothing,
			// and dynamic adapter config for policy backend.
			oopConfig, err := readOOPConfig()
			if err != nil {
				t.Fatalf("cannot read out of process config file: %v", err)
			}
			pbCfg := be.CreateConfigSnippet("", "", policybackend.OutOfProcess)
			gal.ApplyConfigOrFail(
				t,
				istioSystemNs,
				oopConfig,
				testOOPCheckConfig,
				pbCfg)

			retry.UntilSuccessOrFail(t, func() error {
				t.Log("try call mixer check, it should not pass since deny rule has been applied")
				result := mxr.Check(t, map[string]interface{}{
					"context.protocol":      "http",
					"destination.name":      "someworkload",
					"destination.namespace": ns.Name(),
					"response.time":         time.Now(),
					"request.time":          time.Now(),
					"destination.service":   `svc.` + ns.Name(),
					"origin.ip":             []byte{1, 2, 3, 4},
				})

				if result.Succeeded() {
					t.Logf("check passed with result: %v", result.Raw)
					return fmt.Errorf("check should not pass: %v", result.Raw)
				}

				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(time.Second*80))
			gal.DeleteConfigOrFail(t, istioSystemNs, pbCfg, testOOPCheckConfig)
		})
}

// readOOPConfig reads out of process adapter config, which includes dyanmic template for keyval and checknothing,
// and dynamic adapter config for policy backend.
func readOOPConfig() (string, error) {
	oopConfig := ""
	if tmpl, err := file.AsString(checknothingTmpl); err == nil {
		oopConfig += "\n" + tmpl
	} else {
		return "", err
	}
	if tmpl, err := file.AsString(keyvalTmpl); err == nil {
		oopConfig += "\n" + tmpl
	} else {
		return "", err
	}
	if cfg, err := file.AsString(adapterConfig); err == nil {
		oopConfig += "\n" + cfg
	} else {
		return "", err
	}

	return strings.TrimSpace(oopConfig), nil
}

func TestMain(m *testing.M) {
	framework.NewSuite("out_of_process_adapter_test", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		Run()
}

var testOOPKeyValInstace = `
apiVersion: config.istio.io/v1alpha2
kind: instance
metadata:
  name: keyval
  namespace: istio-system
spec:
  template: keyval
  params:
    key: request.headers["user"] | ""
`

var testOOPKeyValGroupRule = `
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: keyval
spec:
  actions:
  - handler: keyval.istio-system
    instances: [ keyval ]
    name: x
  requestHeaderOperations:
  - name: user-group
    values: [ x.output.value ]
`

var testOOPKeyValPathRule = `
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: keyval
spec:
  match: source.labels["istio"] == "ingressgateway"
  actions:
  - handler: keyval.istio-system
    instances: [ keyval ]
  requestHeaderOperations:
  - name: :path
    values: [ '"/status/418"' ]
`

var testOOPCheckConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: checknothing1	
spec:
  template: checknothing
  params:
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: denyrule
spec:
  actions:
  - handler: denyhandler
    instances:
    - checknothing1
`
