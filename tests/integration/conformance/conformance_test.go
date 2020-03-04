// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in conformance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package conformance

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"gopkg.in/yaml.v2"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/conformance"
	"istio.io/istio/pkg/test/conformance/constraint"
	epb "istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/util/structpath"
	"istio.io/istio/pkg/test/util/tmpl"
)

func TestConformance(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		cases, err := loadCases()
		if err != nil {
			ctx.Fatalf("error loading test cases: %v", err)
		}

		gal := galley.NewOrFail(ctx, ctx, galley.Config{CreateClient: true})
		p := pilot.NewOrFail(ctx, ctx, pilot.Config{Galley: gal})

		for _, ca := range cases {
			tst := ctx.NewSubTest(ca.Metadata.Name)

			for _, lname := range ca.Metadata.Labels {
				l, ok := label.Find(lname)
				if !ok {
					ctx.Fatalf("label not found: %v", lname)
				}
				tst = tst.Label(l)
			}

			if ca.Metadata.Exclusive {
				tst.Run(runCaseFn(p, gal, ca))
			} else {
				tst.RunParallel(runCaseFn(p, gal, ca))
			}
		}
	})
}

func runCaseFn(p pilot.Instance, gal galley.Instance, ca *conformance.Test) func(framework.TestContext) {
	return func(ctx framework.TestContext) {
		match := true
	mainloop:
		for _, ename := range ca.Metadata.Environments {
			match = false
			for _, n := range environment.Names() {
				if n.String() == ename && n == ctx.Environment().EnvironmentName() {
					match = true
					break mainloop
				}
			}
		}

		if !match {
			ctx.Skipf("None of the expected environment(s) not found: %v", ca.Metadata.Environments)
		}

		if ca.Metadata.Skip {
			ctx.Skipf("Test is marked as skip")
		}

		// If there are any changes to the mesh config, then capture the original and restore.
		for _, s := range ca.Stages {
			if s.MeshConfig != nil {
				// TODO: Add support to get/set old meshconfig to avoid cross-test interference.
				// originalMeshCfg := gal.GetMeshConfigOrFail(ctx)
				// defer gal.SetMeshConfigOrFail(ctx, originalMeshCfg)
				break
			}
		}

		ns := namespace.NewOrFail(ctx, ctx, namespace.Config{
			Prefix: "conf",
			Inject: true,
		})

		if len(ca.Stages) == 1 {
			runStage(ctx, p, gal, ns, ca.Stages[0])
		} else {
			for i, s := range ca.Stages {
				ctx.NewSubTest(fmt.Sprintf("%d", i)).Run(func(ctx framework.TestContext) {
					runStage(ctx, p, gal, ns, s)
				})
			}
		}
	}
}

func runStage(ctx framework.TestContext, pil pilot.Instance, gal galley.Instance, ns namespace.Instance, s *conformance.Stage) {
	if s.MeshConfig != nil {
		gal.SetMeshConfigOrFail(ctx, *s.MeshConfig)
	}

	i := s.Input
	gal.ApplyConfigOrFail(ctx, ns, i)
	defer func() {
		gal.DeleteConfigOrFail(ctx, ns, i)
	}()

	if s.MCP != nil {
		validateMCPState(ctx, gal, ns, s)
	}
	if s.Traffic != nil {
		validateTraffic(ctx, pil, gal, ns, s)
	}

	// More and different types of validations can go here
}

func validateMCPState(ctx test.Failer, gal galley.Instance, ns namespace.Instance, s *conformance.Stage) {
	p := constraint.Params{
		Namespace: ns.Name(),
	}
	for _, coll := range s.MCP.Constraints {
		gal.WaitForSnapshotOrFail(ctx, coll.Name, func(actuals []*galley.SnapshotObject) error {
			for _, rangeCheck := range coll.Check {
				a := make([]interface{}, len(actuals))
				for i, item := range actuals {
					// Deep copy the item so we can modify the metadata safely
					itemCopy := &galley.SnapshotObject{}
					metadata := *item.Metadata
					itemCopy.Metadata = &metadata
					itemCopy.Body = item.Body
					itemCopy.TypeURL = item.TypeURL

					a[i] = itemCopy

					// Clear out for stable comparison.
					itemCopy.Metadata.CreateTime = nil
					itemCopy.Metadata.Version = ""
					if itemCopy.Metadata.Annotations != nil {
						delete(itemCopy.Metadata.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
						if len(itemCopy.Metadata.Annotations) == 0 {
							itemCopy.Metadata.Annotations = nil
						}
					}
				}

				if err := rangeCheck.ValidateItems(a, p); err != nil {
					return err
				}
			}
			return nil
		})
	}

}

// Returns success for Envoy configs containing routes for all of specified domains.
func domainAcceptFunc(domains []string) func(*envoyAdmin.ConfigDump) (bool, error) {
	return func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		validator := structpath.ForProto(cfg)
		const q = "{.configs[*].dynamicRouteConfigs[*].routeConfig.virtualHosts[*].domains[?(@ == %q)]}"
		for _, domain := range domains {
			// TODO(qfel): Figure out how to get rid of the loop.
			if err := validator.Exists(q, domain).Check(); err != nil {
				return false, err
			}
		}
		return true, nil
	}
}

func virtualServiceHosts(ctx test.Failer, resources string) []string {
	// TODO(qfel): We should parse input early and work on structured data here. Or at least if some
	// other code path needs it.
	var inputs []map[interface{}]interface{}
	for _, inputYAML := range strings.Split(resources, "\n---\n") {
		var input map[interface{}]interface{}
		if err := yaml.Unmarshal([]byte(inputYAML), &input); err != nil {
			ctx.Fatalf("yaml.Unmarshal: %v", err)
		}
		inputs = append(inputs, input)
	}

	var vHosts []string
	for _, res := range inputs {
		if res["apiVersion"] != "networking.istio.io/v1alpha3" || res["kind"] != "VirtualService" {
			continue
		}
		spec := res["spec"].(map[interface{}]interface{})
		hosts := spec["hosts"].([]interface{})
		for _, h := range hosts {
			vHosts = append(vHosts, h.(string))
		}
	}
	return vHosts
}

func validateTraffic(t framework.TestContext, pil pilot.Instance, gal galley.Instance, ns namespace.Instance, stage *conformance.Stage) {
	echos := make([]echo.Instance, len(stage.Traffic.Services))
	b := echoboot.NewBuilderOrFail(t, t)
	for i, svc := range stage.Traffic.Services {
		var ports []echo.Port
		for _, p := range svc.Ports {
			ports = append(ports, echo.Port{
				Name:        p.Name,
				Protocol:    protocol.Instance(p.Protocol),
				ServicePort: int(p.ServicePort),
			})
		}
		b = b.With(&echos[i], echo.Config{
			Galley:    gal,
			Pilot:     pil,
			Service:   svc.Name,
			Namespace: ns,
			Ports:     ports,
		})
	}
	if err := b.Build(); err != nil {
		t.Fatal(err)
	}

	services := make(map[string]echo.Instance)
	for i, svc := range echos {
		services[stage.Traffic.Services[i].Name] = svc
		svc.WaitUntilCallableOrFail(t, echos...)
	}

	ready := make(map[string]bool)
	vHosts := virtualServiceHosts(t, stage.Input)
	for i, call := range stage.Traffic.Calls {
		call.URL = tmpl.EvaluateOrFail(t, call.URL, constraint.Params{
			Namespace: ns.Name(),
		})
		t.NewSubTest(strconv.Itoa(i)).Run(func(t framework.TestContext) {
			hostname, err := wildcardToRegexp(call.Response.Callee)
			if err != nil {
				t.Fatalf("Internal error: regexp.Compile: %v", err)
			}
			caller := services[call.Caller]
			if !ready[call.Caller] && false {
				t.Logf("Waiting for sidecar(s) for %s to contain domains: %s", call.Caller, strings.Join(vHosts, ", "))
				for _, w := range caller.WorkloadsOrFail(t) {
					w.Sidecar().WaitForConfigOrFail(t, domainAcceptFunc(vHosts))
				}
				ready[call.Caller] = true
			}
			u, err := url.Parse(call.URL)
			if err != nil {
				t.Fatalf("Parse call URL: %v", err)
			}
			workload := caller.WorkloadsOrFail(t)[0]

			validateWithRedo(t, func(ctx context.Context) bool {
				responses, err := workload.ForwardEcho(ctx, &epb.ForwardEchoRequest{
					Url:   call.URL,
					Count: 1,
					Headers: []*epb.Header{{
						Key:   "Host",
						Value: u.Host, // TODO(qfel): Why cannot echo infer it from URL?
					}},
				})
				if err != nil {
					t.Logf("Initiating test call: %v", err)
					return false
				}

				if len(responses) != 1 {
					t.Logf("Received %d responses, want 1", len(responses))
					return false
				}
				resp := responses[0]
				var fail bool
				if resp.Code != strconv.Itoa(call.Response.Code) {
					t.Logf("Responded with %s, want %d", resp.Code, call.Response.Code)
					fail = true
				}
				if !hostname.MatchString(resp.Hostname) {
					t.Logf("Callee %q doesn't match %q", resp.Hostname, call.Response.Callee)
					fail = true
				}
				return !fail
			})
		})
	}
}

func validateWithRedo(t framework.TestContext, f func(context.Context) bool) {
	const (
		pollDelay           = time.Second // How much to wait after a failed attempt.
		firstSuccessTimeout = time.Minute
		// After the traffic flows successfully for the first time, repeat according to the parameters
		// below.
		redoAttempts = 10
		redoTimeout  = 20 * time.Second
		redoFraction = 0.9 // Fail if not enough attempts succeed.
	)

	ctx, cfn := context.WithTimeout(context.Background(), firstSuccessTimeout)
	defer cfn()
	for {
		if f(ctx) {
			break
		}
		if sleepCtx(ctx, pollDelay) != nil {
			t.Fatal("No successful attempt")
		}
	}

	var passed int
	ctx, cfn = context.WithTimeout(context.Background(), redoTimeout)
	defer cfn()
	for i := 0; i < redoAttempts; i++ {
		start := time.Now()
		res := f(ctx)
		t.Logf("Traffic attempt #%d: %v in %s", i, res, time.Since(start))
		if res {
			passed++
		} else if i+1 < redoAttempts {
			// No need to check errors. If ctx times out, further calls to f will time out.
			_ = sleepCtx(ctx, pollDelay)
		}
	}
	fr := float64(passed) / float64(redoAttempts)
	t.Logf("%d/%d (%f) attempts succeeded", passed, redoAttempts, fr)
	if fr < redoFraction {
		t.Errorf("Success rate is %f, need at least %f", fr, redoFraction)
	}
}

func sleepCtx(ctx context.Context, duration time.Duration) error {
	t := time.NewTimer(duration)
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		t.Stop()
		return ctx.Err()
	}
}

func wildcardToRegexp(s string) (*regexp.Regexp, error) {
	s = fmt.Sprintf("^%s$", regexp.QuoteMeta(s))
	return regexp.Compile(strings.Replace(s, `\*`, ".*", -1))
}
