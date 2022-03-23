//go:build integ
// +build integ

//  Copyright Istio Authors
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

package prometheus

import (
	"fmt"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/telemetry"
)

// QueryPrometheus queries prometheus and returns the result once the query stabilizes
func QueryPrometheus(t framework.TestContext, cluster cluster.Cluster, query prometheus.Query, promInst prometheus.Instance) (string, error) {
	t.Helper()
	t.Logf("query prometheus with: %v", query)

	val, err := promInst.Query(cluster, query)
	if err != nil {
		return "", err
	}
	got, err := prometheus.Sum(val)
	if err != nil {
		t.Logf("value: %s", val.String())
		return "", fmt.Errorf("could not find metric value: %v", err)
	}
	t.Logf("get value %v", got)
	return val.String(), nil
}

func ValidateMetric(t framework.TestContext, cluster cluster.Cluster, prometheus prometheus.Instance, query prometheus.Query, want float64) {
	t.Helper()
	err := retry.UntilSuccess(func() error {
		got, err := prometheus.QuerySum(cluster, query)
		t.Logf("%s: %f", query.Metric, got)
		if err != nil {
			return err
		}
		if got < want {
			return fmt.Errorf("bad metric value: got %f, want at least %f", got, want)
		}
		return nil
	}, retry.Delay(time.Second), retry.Timeout(time.Second*20))
	if err != nil {
		util.PromDiff(t, prometheus, cluster, query)
		t.Fatal(err)
	}
}
