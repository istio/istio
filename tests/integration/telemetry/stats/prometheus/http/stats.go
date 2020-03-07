// Copyright 2020 Istio Authors. All Rights Reserved.
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

package prometheus

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/mixer"
	promUtil "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

var (
	ist            istio.Instance
	bookinfoNsInst namespace.Instance
	galInst        galley.Instance
	ingInst        ingress.Instance
	promInst       prometheus.Instance
)

// GetIstioInstance gets Istio instance.
func GetIstioInstance() *istio.Instance {
	return &ist
}

// GetBookinfoNamespaceInstance gets bookinfo instance.
func GetBookinfoNamespaceInstance() namespace.Instance {
	return bookinfoNsInst
}

// GetIngressInstance gets ingress instance.
func GetIngressInstance() ingress.Instance {
	return ingInst
}

// GetPromInstance gets prometheus instance.
func GetPromInstance() prometheus.Instance {
	return promInst
}

// TestStatsFilter includes common test logic for stats and mx exchange filters running
// with nullvm and wasm runtime.
func TestStatsFilter(t *testing.T) {
	framework.NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			ingress := GetIngressInstance()
			addr := ingress.HTTPAddress()
			url := fmt.Sprintf("http://%s/productpage", addr.String())
			sourceQuery, destinationQuery := buildQuery()
			retry.UntilSuccessOrFail(t, func() error {
				util.SendTraffic(ingress, t, "Sending traffic", url, "", 200)
				// Query client side metrics
				if err := promUtil.QueryPrometheus(t, sourceQuery, GetPromInstance()); err != nil {
					t.Logf("prometheus values for istio_requests_total: \n%s", util.PromDump(promInst, "istio_requests_total"))
					return err
				}
				if err := promUtil.QueryPrometheus(t, destinationQuery, GetPromInstance()); err != nil {
					t.Logf("prometheus values for istio_requests_total: \n%s", util.PromDump(promInst, "istio_requests_total"))
					return err
				}
				return nil
			}, retry.Delay(3*time.Second), retry.Timeout(80*time.Second))
		})
}

// TestSetup set up bookinfo app for stats testing.
func TestSetup(ctx resource.Context) (err error) {
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
	err = galInst.ApplyConfig(
		bookinfoNsInst,
		bookingfoGatewayFile,
	)
	if err != nil {
		return
	}
	return nil
}

func buildQuery() (sourceQuery, destinationQuery string) {
	bookinfoNsInst := GetBookinfoNamespaceInstance()
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
