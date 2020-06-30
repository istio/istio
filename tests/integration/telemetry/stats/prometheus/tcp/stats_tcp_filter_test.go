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

package tcp

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/mixer"
	util_prometheus "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

const (
	cleanupFilterConfig = "testdata/cleanup.yaml"
)

var (
	ist        istio.Instance
	bookinfoNs namespace.Instance
	ing        ingress.Instance
	prom       prometheus.Instance
)

func TestTcpMetric(t *testing.T) { // nolint:interfacer
	framework.
		NewTest(t).
		Features("observability.telemetry.stats.prometheus.tcp").
		Run(func(ctx framework.TestContext) {
			addr := ing.HTTPAddress()
			url := fmt.Sprintf("http://%s/productpage", addr.String())
			ctx.Config().ApplyYAMLOrFail(
				t,
				bookinfoNs.Name(),
				bookinfo.GetDestinationRuleConfigFileOrFail(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
				bookinfo.NetworkingTCPDbRule.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			)
			defer ctx.Config().DeleteYAML(
				bookinfoNs.Name(),
				bookinfo.GetDestinationRuleConfigFileOrFail(t, ctx).LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
				bookinfo.NetworkingTCPDbRule.LoadWithNamespaceOrFail(t, bookinfoNs.Name()),
			)

			systemNM := namespace.ClaimSystemNamespaceOrFail(ctx, ctx)
			cleanup, err := file.AsString(cleanupFilterConfig)
			if err != nil {
				t.Errorf("unable to load config %s, err:%v", cleanupFilterConfig, err)
			}

			ctx.Config().ApplyYAMLOrFail(
				t,
				systemNM.Name(),
				cleanup,
			)
			defer ctx.Config().DeleteYAML(
				systemNM.Name(),
				cleanup,
			)

			util.AllowRuleSync(t)

			destinationQuery := buildQuery()
			retry.UntilSuccessOrFail(t, func() error {
				util.SendTraffic(ing, t, "Sending traffic", url, "", 200)
				// TODO(gargnupur): Use TCP metrics like in Telemetry V1 (https://github.com/istio/istio/issues/20283)
				if err := util_prometheus.QueryPrometheus(t, destinationQuery, prom); err != nil {
					t.Logf("prometheus values for istio_tcp_connections_opened_total: \n%s", util.PromDump(prom, "istio_tcp_connections_opened_total"))
					return err
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
		})
}

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, nil)).
		Setup(testsetup).
		Run()
}

func testsetup(ctx resource.Context) (err error) {
	bookinfoNs, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-bookinfo",
		Inject: true,
	})
	if err != nil {
		return
	}
	if _, err := bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookInfo}); err != nil {
		return err
	}
	if _, err = bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookinfoRatingsv2}); err != nil {
		return err
	}
	if _, err = bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNs, Cfg: bookinfo.BookinfoDB}); err != nil {
		return err
	}
	ing, err = ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return err
	}
	prom, err = prometheus.New(ctx, prometheus.Config{})
	if err != nil {
		return err
	}
	yamlText, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNs.Name())
	if err != nil {
		return err
	}
	err = ctx.Config().ApplyYAML(bookinfoNs.Name(), yamlText)
	if err != nil {
		return err
	}

	return nil
}

func buildQuery() (destinationQuery string) {
	destinationQuery = `istio_tcp_connections_opened_total{reporter="destination",`
	labels := map[string]string{
		"request_protocol":               "tcp",
		"destination_service_name":       "mongodb",
		"destination_canonical_revision": "v1",
		"destination_canonical_service":  "mongodb",
		"destination_app":                "mongodb",
		"destination_version":            "v1",
		"destination_workload_namespace": bookinfoNs.Name(),
		"destination_service_namespace":  bookinfoNs.Name(),
		"source_app":                     "ratings",
		"source_version":                 "v2",
		"source_workload":                "ratings-v2",
		"source_workload_namespace":      bookinfoNs.Name(),
	}
	for k, v := range labels {
		destinationQuery += fmt.Sprintf(`%s=%q,`, k, v)
	}
	destinationQuery += "}"
	return
}
