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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/periodic"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

var (
	ist         istio.Instance
	bookinfoNs  namespace.Instance
	ratelimitNs namespace.Instance
	ing         ingress.Instance
)

func TestRateLimiting_DefaultLessThanOverride(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			res := sendTraffic(ing, t, "Sending traffic...", 300)
			got429s := float64(res.RetCodes[http.StatusTooManyRequests])

			if got429s <= 0 {
				t.Errorf("Bad metric value for rate-limited requests (429s): got %f, want at least 1", got429s)
			}
		})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, nil)).
		Setup(testSetup).
		Run()
}

func testSetup(ctx resource.Context) (err error) {
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

	ing = ist.IngressFor(ctx.Clusters().Default())

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
	err = ctx.Config().ApplyYAML(bookinfoNs.Name(),
		bookinfoGatewayFile,
		destinationRuleFile,
		virtualServiceFile)
	if err != nil {
		return
	}

	ratelimitNs, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-ratelimit",
	})
	if err != nil {
		return
	}

	err = setupEnvoyFilter(ratelimitNs, ctx)
	if err != nil {
		return
	}

	yamlContent, err := ioutil.ReadFile("testdata/ratelimitservice.yaml")
	if err != nil {
		return
	}

	err = ctx.Config().ApplyYAML(ratelimitNs.Name(),
		string(yamlContent),
	)
	if err != nil {
		return
	}
	// Wait for redis and ratelimit service to be up.
	fetchFn := kube.NewPodFetch(ctx.Clusters().Default(), ratelimitNs.Name(), "app=redis")
	if _, err = kube.WaitUntilPodsAreReady(fetchFn); err != nil {
		return
	}
	fetchFn = kube.NewPodFetch(ctx.Clusters().Default(), ratelimitNs.Name(), "app=ratelimit")
	if _, err = kube.WaitUntilPodsAreReady(fetchFn); err != nil {
		return
	}

	// For envoy filter changes to sync.
	time.Sleep(time.Second * 15)

	return nil
}

func setupEnvoyFilter(ratelimitNs namespace.Instance, ctx resource.Context) error {
	content, err := ioutil.ReadFile("testdata/enable_envoy_ratelimit.yaml")
	if err != nil {
		return err
	}
	con := string(content)

	con = strings.Replace(con, "ratelimit.default.svc.cluster.local",
		"ratelimit."+ratelimitNs.Name()+".svc.cluster.local", -1)

	ns, err := namespace.Claim(ctx, ist.Settings().SystemNamespace, true)
	if err != nil {
		scopes.Framework.Infof("err: %v", err)
		return err
	}
	err = ctx.Config().ApplyYAML(ns.Name(), con)
	if err != nil {
		scopes.Framework.Infof("err: %v", err)
		return err
	}
	return nil
}

func sendTraffic(ingress ingress.Instance, t *testing.T, msg string, calls int64) *fhttp.HTTPRunnerResults {
	t.Log(msg)

	addr := ingress.HTTPAddress()
	url := fmt.Sprintf("http://%s/productpage", addr.String())

	// run at a high enough QPS (here 10) to ensure that enough
	// traffic is generated to trigger 429s from the 1 QPS rate limit rule
	opts := fhttp.HTTPRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			QPS:        10,
			Exactly:    calls,     // will make exactly 300 calls, so run for about 30 seconds
			NumThreads: 5,         // get the same number of calls per connection (300/5=60)
			Out:        os.Stderr, // Only needed because of log capture issue
		},
		HTTPOptions: fhttp.HTTPOptions{
			URL: url,
		},
	}

	// productpage should still return 200s when ratings is rate-limited.
	res, err := fhttp.RunHTTPTest(&opts)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}
	return res
}
