// Copyright 2019 Istio Authors. All Rights Reserved.
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

package promtheus

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/mixer"
)

const (
	statsFilterConfig            = "testdata/stats_filter.yaml"
	metadataExchangeFilterConfig = "testdata/metadata_exchange_filter.yaml"
)

var (
	ist            istio.Instance
	bookinfoNsInst namespace.Instance
	galInst        galley.Instance
	ingInst        ingress.Instance
	promInst       prometheus.Instance
)

func getIstioInstance() *istio.Instance {
	return &ist
}

func getBookinfoNamespaceInstance() namespace.Instance {
	return bookinfoNsInst
}

func getIngressInstance() ingress.Instance {
	return ingInst
}

func getPromInstance() prometheus.Instance {
	return promInst
}

func queryPrometheus(t *testing.T, query string) error {
	promInst := getPromInstance()
	t.Logf("query prometheus with: %v", query)
	val, err := promInst.WaitForQuiesce(query)
	if err != nil {
		return err
	}
	got, err := promInst.Sum(val, nil)
	if err != nil {
		t.Logf("value: %s", val.String())
		return fmt.Errorf("could not find metric value: %v", err)
	}
	t.Logf("get value %v", got)
	return nil
}

func buildQuery() (sourceQuery, destinationQuery string) {
	bookinfoNsInst := getBookinfoNamespaceInstance()
	sourceQuery = `istio_requests_total{reporter="source",`
	destinationQuery = `istio_requests_total{reporter="destination",`
	labels := map[string]string{
		"request_protocol":               "http",
		"response_code":                  "200",
		"destination_app":                "reviews",
		"destination_version":            "v1",
		"destination_service":            "reviews." + bookinfoNsInst.Name() + ".svc.cluster.local",
		"destination_service_name":       "reviews",
		"destination_workload_namespace": bookinfoNsInst.Name(),
		"destination_service_namespace":  bookinfoNsInst.Name(),
		"source_app":                     "productpage",
		"source_version":                 "v1",
		"source_workload":                "productpage-v1",
		"source_workload_namespace":      bookinfoNsInst.Name(),
	}
	for k, v := range labels {
		sourceQuery += fmt.Sprintf(`%s=%q,`, k, v)
		destinationQuery += fmt.Sprintf(`%s=%q,`, k, v)
	}
	sourceQuery += "}"
	destinationQuery += "}"
	return
}

// TestStatsFilter verifies the stats filter could emit expected client and server side metrics.
// This test focuses on stats filter and metadata exchange filter could work coherently with
// proxy bootstrap config. To avoid flake, it does not verify correctness of metrics, which
// should be covered by integration test in proxy repo.
func TestStatsFilter(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ingress := getIngressInstance()
			addr := ingress.HTTPAddress()
			url := fmt.Sprintf("http://%s/productpage", addr.String())
			sourceQuery, destinationQuery := buildQuery()
			retry.UntilSuccessOrFail(t, func() error {
				util.SendTraffic(ingress, t, "Sending traffic", url, "", 200)
				// Query client side metrics
				if err := queryPrometheus(t, sourceQuery); err != nil {
					t.Logf("prometheus values for istio_requests_total: \n%s", util.PromDump(promInst, "istio_requests_total"))
					return err
				}
				if err := queryPrometheus(t, destinationQuery); err != nil {
					t.Logf("prometheus values for istio_requests_total: \n%s", util.PromDump(promInst, "istio_requests_total"))
					return err
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite("stats_filter_test", m).
		RequireEnvironment(environment.Kube).
		Label(label.CustomSetup).
		SetupOnEnv(environment.Kube, istio.Setup(getIstioInstance(), setupConfig)).
		Setup(testSetup).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// disable telemetry and mixer filter
	cfg.Values["global.disablePolicyChecks"] = "true"
	cfg.Values["global.MixerCheckServer"] = ""
	cfg.Values["global.MixerReportServer"] = ""
	cfg.Values["mixer.telemetry.enabled"] = "false"
}

func testSetup(ctx resource.Context) (err error) {
	galInst, err = galley.New(ctx, galley.Config{})
	if err != nil {
		return
	}
	bookinfoNsInst, err = namespace.New(ctx, namespace.Config{
		Prefix: "istio-bookinfo",
		Inject: true,
	})
	if err != nil {
		return
	}
	if _, err = bookinfo.Deploy(ctx, bookinfo.Config{Namespace: bookinfoNsInst, Cfg: bookinfo.BookInfo}); err != nil {
		return
	}
	ingInst, err = ingress.New(ctx, ingress.Config{Istio: ist})
	if err != nil {
		return
	}
	promInst, err = prometheus.New(ctx)
	if err != nil {
		return
	}
	bookingfoGatewayFile, err := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespace(bookinfoNsInst.Name())
	if err != nil {
		return
	}
	// Apply metadata exchange filter and stats filter.
	statsFilterFile, err := file.AsString(statsFilterConfig)
	if err != nil {
		return
	}
	exchangeFilterFile, err := file.AsString(metadataExchangeFilterConfig)
	if err != nil {
		return
	}
	err = galInst.ApplyConfig(
		bookinfoNsInst,
		bookingfoGatewayFile,
		statsFilterFile,
		exchangeFilterFile,
	)
	if err != nil {
		return
	}
	return nil
}
