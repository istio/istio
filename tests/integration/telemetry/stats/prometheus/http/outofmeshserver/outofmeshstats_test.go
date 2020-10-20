// +build integ
// Copyright Istio Authors. All Rights Reserved.
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

package outofmeshserver

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus/http"
)

// TestOutOfMeshServerStats tests client stats for request sending to server workload that is out of mesh.
func TestOutOfMeshServerStats(t *testing.T) {
	common.TestStatsFilter(t, features.Feature("observability.telemetry.stats.prometheus.http.nullvm"),
		common.QueryOptions{
			QueryClient:    true,
			QueryServer:    false,
			QueryApp:       false,
			BuildQueryFunc: buildQuery,
		},
	)
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(common.GetIstioInstance(), nil)).
		Setup(testSetup).
		Run()
}

func testSetup(ctx resource.Context) (err error) {
	return common.TestSetup(ctx, common.SetupOption{ServerSidecar: false})
}

func buildQuery() (sourceQuery, destinationQuery, appQuery string) {
	ns := common.GetAppNamespace()
	sourceQuery = ""
	appQuery = ""
	sourceQuery = `istio_requests_total{reporter="source",`
	labels := map[string]string{
		"request_protocol":               "http",
		"response_code":                  "200",
		"destination_app":                "unknown",
		"destination_version":            "unknown",
		"destination_canonical_service":  "server-v1",
		"destination_canonical_revision": "latest",
		"destination_service":            "server." + ns.Name() + ".svc.cluster.local",
		"destination_service_name":       "server",
		"destination_workload_namespace": ns.Name(),
		"destination_service_namespace":  ns.Name(),
		"source_app":                     "client",
		"source_version":                 "v1",
		"source_canonical_service":       "client",
		"source_canonical_revision":      "v1",
		"source_workload":                "client-v1",
		"source_workload_namespace":      ns.Name(),
	}
	for k, v := range labels {
		sourceQuery += fmt.Sprintf(`%s=%q,`, k, v)
	}
	sourceQuery += "}"
	return
}
