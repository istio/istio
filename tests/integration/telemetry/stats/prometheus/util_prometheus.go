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
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/util/retry"
)

// QueryPrometheus queries prometheus and returns the result once the query stabilizes
func QueryPrometheus(t *testing.T, query string, promInst prometheus.Instance) error {
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

// QueryFirstPrometheus queries prometheus and returns the result once a timeseries exists
func QueryFirstPrometheus(t *testing.T, query string, promInst prometheus.Instance) error {
	t.Logf("query prometheus with: %v", query)
	val, err := promInst.WaitForOneOrMore(query)
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

func ValidateMetric(t *testing.T, prometheus prometheus.Instance, query, metricName string, want float64) {
	var got float64
	retry.UntilSuccessOrFail(t, func() error {
		var err error
		got, err = getMetric(t, prometheus, query, metricName)
		return err
	}, retry.Delay(time.Second), retry.Timeout(2*time.Minute))

	t.Logf("%s: %f", metricName, got)
	if got < want {
		t.Logf("prometheus values for %s:\n%s", metricName, PromDump(prometheus, metricName))
		t.Errorf("bad metric value: got %f, want at least %f", got, want)
	}
}

func getMetric(t *testing.T, prometheus prometheus.Instance, query, metricName string) (float64, error) {
	t.Helper()

	t.Logf("prometheus query: %s", query)
	value, err := prometheus.WaitForQuiesce(query)
	if err != nil {
		return 0, fmt.Errorf("could not get metrics from prometheus: %v", err)
	}

	got, err := prometheus.Sum(value, nil)
	if err != nil {
		t.Logf("value: %s", value.String())
		t.Logf("prometheus values for %s:\n%s", metricName, PromDump(prometheus, metricName))
		return 0, fmt.Errorf("could not find metric value: %v", err)
	}

	return got, nil
}

// promDump gets all of the recorded values for a metric by name and generates a report of the values.
// used for debugging of failures to provide a comprehensive view of traffic experienced.
func PromDump(prometheus prometheus.Instance, metric string) string {
	return PromDumpWithAttributes(prometheus, metric, nil)
}

// promDumpWithAttributes is used to get all of the recorded values of a metric for particular attributes.
// Attributes have to be of format %s=\"%s\"
// nolint: unparam
func PromDumpWithAttributes(prometheus prometheus.Instance, metric string, attributes []string) string {
	if value, err := prometheus.WaitForQuiesce(fmt.Sprintf("%s{%s}", metric, strings.Join(attributes, ", "))); err == nil {
		return value.String()
	}

	return ""
}
