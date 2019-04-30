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
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/httpbin"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/mixer"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/policybackend"
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

func TestRouteDirective(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	ctx.RequireOrSkip(t, environment.Kube)
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

	// Apply all configs needed by out of process adapter, which includes all needed dynamic templates,
	// adapter config, instances and handlers.
	g.ApplyConfigOrFail(t, istioSystemNs,
		oopConfig,
		testOOPKeyValInstace,
		be.CreateConfigSnippet("policy-backend", istioSystemNs.Name(), policybackend.OutOfProcess),
	)
	g.ApplyConfigOrFail(t, httpbinNs,
		httpbin.NetworkingHttpbinGateway.LoadWithNamespaceOrFail(t, httpbinNs.Name()))
	ing := ingress.NewOrFail(t, ctx, ingress.Config{Istio: ist})

	var response ingress.CallResponse

	t.Log("visit httpbin")
	retry.UntilSuccessOrFail(t, func() error {
		response, err = ing.Call("/")
		if err != nil {
			return fmt.Errorf("unable to connect to httpbin: %v", err)
		}
		return nil
	}, retry.Delay(3*time.Second), retry.Timeout(30*time.Second))

	// Test rule that adds header.
	t.Log("apply rule to add 'group' header")
	g.ApplyConfigOrFail(t, istioSystemNs, testOOPKeyValGroupRule)

	// Retry visiting httpbin until gets "user-group" header
	retry.UntilSuccessOrFail(t, func() error {
		response, err = ing.CallWithHeaders("/headers", map[string]string{"user": "jason"})
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
			if strings.ToLower(key) == "user-group" && strings.ToLower(val.(string)) == "admin" {
				return nil
			}
		}
		return fmt.Errorf("cannot find user-group header in request headers: %v", headers)
	}, retry.Delay(3*time.Second), retry.Timeout(40*time.Second))

	t.Log("find user-group: admin header")
	g.DeleteConfigOrFail(t, istioSystemNs, testOOPKeyValGroupRule)

	// Test header editing and route directive.
	t.Log("apply rule to edit path header")
	g.ApplyConfigOrFail(t, istioSystemNs, testOOPKeyValPathRule)
	retry.UntilSuccessOrFail(t, func() error {
		response, err = ing.Call("/headers")
		if err != nil {
			return fmt.Errorf("unable to connect to httpbin: %v", err)
		}
		if response.Code != statusTeapot {
			return fmt.Errorf("httpbin does not return teapot: %v", response.Code)
		}
		return nil
	}, retry.Delay(3*time.Second), retry.Timeout(40*time.Second))
	t.Log("httpbin returns status 418")
	g.DeleteConfigOrFail(t, istioSystemNs, testOOPKeyValPathRule)
}

func TestCheck(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Done(t)

	gal := galley.NewOrFail(t, ctx, galley.Config{})
	mxr := mixer.NewOrFail(t, ctx, mixer.Config{
		Galley: gal,
	})
	be := policybackend.NewOrFail(t, ctx)

	ns := namespace.NewOrFail(t, ctx, "testcheck-deny-oop", false)
	istioSystemNs := namespace.ClaimOrFail(t, ctx, "istio-system")

	// Get out of process adapter related config, which includes dyanmic template for keyval and checknothing,
	// and dynamic adapter config for policy backend.
	oopConfig, err := readOOPConfig()
	if err != nil {
		t.Fatalf("cannot read out of process config file: %v", err)
	}

	gal.ApplyConfigOrFail(
		t,
		istioSystemNs,
		oopConfig,
		testOOPCheckConfig,
		be.CreateConfigSnippet("", "", policybackend.OutOfProcess))

	retry.UntilSuccessOrFail(t, func() error {
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
			return fmt.Errorf("check should not pass: %v", result.Raw)
		}

		return nil
	}, retry.Timeout(time.Second*40))
}

// readOOPConfig reads out of process adapter config, which includes dyanmic template for keyval and checknothing,
// and dynamic adapter config for policy backend.
func readOOPConfig() (string, error) {
	oopConfig := ""
	if tmpl, err := test.ReadConfigFile(checknothingTmpl); err == nil {
		oopConfig += "\n" + tmpl
	} else {
		return "", err
	}
	if tmpl, err := test.ReadConfigFile(keyvalTmpl); err == nil {
		oopConfig += "\n" + tmpl
	} else {
		return "", err
	}
	if cfg, err := test.ReadConfigFile(adapterConfig); err == nil {
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
