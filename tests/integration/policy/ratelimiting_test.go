//  Copyright 2020 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package policy

import (
	"io/ioutil"
	"math"
	"net/http"

	"testing"

	"strings"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
	util "istio.io/istio/tests/integration/mixer"
	"time"
)

var (
	ist         istio.Instance
	bookinfoNs  namespace.Instance
	ratelimitNs namespace.Instance
	g           galley.Instance
	ing         ingress.Instance
)

func TestRateLimiting_DefaultLessThanOverride(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			util.AllowRuleSync(t)

			err := setupEnvoyFilterOrFail(t, ratelimitNs, ctx)
			if err != nil {
				return
			}

			res := util.SendTraffic(ing, t, "Sending traffic...", "", "", 300)
			totalReqs := float64(res.DurationHistogram.Count)
			got429s := float64(res.RetCodes[http.StatusTooManyRequests])
			actualDuration := res.ActualDuration.Seconds() // can be a bit more than requested

			// Sending 10 requests. Ratelimiting is set to 1 request per minute for productpage.
			want200s := 1.0
			// everything in excess of 200s should be 429s (ideally)
			want429s := totalReqs - want200s
			t.Logf("Expected Totals: 200s: %f (%f rps), 429s: %f (%f rps)", want200s, want200s/actualDuration,
				want429s, want429s/actualDuration)

			// check resource exhausted
			want429s = math.Floor(want429s * 0.97)
			if got429s < want429s {
				t.Errorf("Bad metric value for rate-limited requests (429s): got %f, want at least %f", got429s,
					want429s)
			}
		})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("envoy_policy_ratelimit", m).
		Label(label.CustomSetup).
		RequireEnvironment(environment.Kube).
		RequireSingleCluster().
		SetupOnEnv(environment.Kube, istio.Setup(&ist, setupConfig)).
		Setup(testsetup).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.Values["telemetry.enabled"] = "true"
	cfg.Values["telemetry.v1.enabled"] = "false"
	cfg.Values["telemetry.v2.enabled"] = "true"
}

func testsetup(ctx resource.Context) (err error) {
	bookinfoNs, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-bookinfo",
		Inject: true,
	})
	if err != nil {
		return
	}
	if _, err = bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo}); err != nil {
		return
	}
	g, err = galley.New(ctx, galley.Config{})
	if err != nil {
		return
	}

	ing, err = ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return
	}

	bookinfoGatewayFile, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNs.Name())
	if err != nil {
		return
	}
	destinationRule, err := bookinfo.GetDestinationRuleConfigFile(ctx)
	if err != nil {
		return
	}
	destinationRuleFile, err := destinationRule.LoadWithNamespace(bookinfoNs.Name())
	if err != nil {
		return
	}
	virtualServiceFile, err := bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespace(bookinfoNs.Name())
	if err != nil {
		return
	}
	err = g.ApplyConfig(bookinfoNs,
		bookinfoGatewayFile,
		destinationRuleFile,
		virtualServiceFile)
	if err != nil {
		return
	}

	//TODO(gargnuur): Make this a component in test pkg framework and wait for pods to
	// come up rather than sleep.
	ratelimitNs, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-ratelimit",
	})
	if err != nil {
		return
	}

	yamlContent, err := ioutil.ReadFile("testdata/ratelimitservice.yaml")
	if err != nil {
		return
	}

	err = g.ApplyConfig(ratelimitNs,
		string(yamlContent),
	)
	if err != nil {
		return
	}

	time.Sleep(time.Second * 30)

	return nil
}

func setupEnvoyFilterOrFail(t *testing.T, ratelimitNs namespace.Instance, ctx resource.Context) error {
	content, err := ioutil.ReadFile("testdata/enable_envoy_ratelimit.yaml")
	if err != nil {
		return err
	}
	con := string(content)

	con = strings.Replace(con, "ratelimit.default.svc.cluster.local",
		"ratelimit."+ratelimitNs.Name()+".svc.cluster.local", -1)

	ns, err := namespace.Claim(ctx, ist.Settings().SystemNamespace, true)
	if err != nil {
		return err
	}
	err = g.ApplyConfig(ns, con)
	if err != nil {
		return err
	}
	return nil
}
