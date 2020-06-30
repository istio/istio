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

package mixer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/periodic"

	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test/framework/components/ingress"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/tests/util"
)

const (
	destLabel         = "destination_service"
	responseCodeLabel = "response_code"
	reporterLabel     = "reporter"
)

func GetDestinationLabel() string {
	return destLabel
}

func GetResponseCodeLabel() string {
	return responseCodeLabel
}

func GetReporterCodeLabel() string {
	return reporterLabel
}

func VisitProductPage(ing ingress.Instance, timeout time.Duration, wantStatus int, t *testing.T) error {
	start := time.Now()
	endpointIP := ing.HTTPAddress()
	for {
		response, err := ing.Call(ingress.CallOptions{
			Host:     "",
			Path:     "/productpage",
			CallType: ingress.PlainText,
			Address:  endpointIP})
		if err != nil {
			t.Logf("Unable to connect to product page: %v", err)
		}

		status := response.Code
		if status == wantStatus {
			t.Logf("Got %d response from product page!", wantStatus)
			return nil
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("could not retrieve product page in %v: Last status: %v", timeout, status)
		}

		time.Sleep(3 * time.Second)
	}
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

func Fqdn(service, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", service, namespace)
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

func SendTraffic(ingress ingress.Instance, t *testing.T, msg, url, extraHeader string, calls int64) *fhttp.HTTPRunnerResults {
	t.Log(msg)
	if url == "" {
		addr := ingress.HTTPAddress()
		url = fmt.Sprintf("http://%s/productpage", addr.String())
	}

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
	if extraHeader != "" {
		opts.HTTPOptions.AddAndValidateExtraHeader(extraHeader)
	}
	// productpage should still return 200s when ratings is rate-limited.
	res, err := fhttp.RunHTTPTest(&opts)
	if err != nil {
		t.Fatalf("Generating traffic via fortio failed: %v", err)
	}
	return res
}

func SendTrafficAndWaitForExpectedStatus(ingress ingress.Instance, t *testing.T, msg, url string, calls int64,
	httpStatusCode int) {
	retry := util.Retrier{
		BaseDelay: 15 * time.Second,
		Retries:   3,
	}

	retryFn := func(_ context.Context, i int) error {
		res := SendTraffic(ingress, t, msg, url, "", calls)
		// Verify you get specified http return code.
		if float64(res.RetCodes[httpStatusCode]) == 0 {
			return fmt.Errorf("could not get %v status", httpStatusCode)
		}
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		t.Fatalf("Failed with err: %v", err)
	}
}

func GetAndValidateAccessLog(ns namespace.Instance, t *testing.T, labelSelector, container string, validate func(string) error) {
	retry := util.Retrier{
		BaseDelay: 15 * time.Second,
		Retries:   3,
		MaxDelay:  30 * time.Second,
	}

	retryFn := func(_ context.Context, i int) error {
		// Different kubectl versions seem to return different amounts of logs. To ensure we get them all, set tail to a large number
		content, err := shell.Execute(false, "kubectl logs -n %s -l %s -c %s --tail=10000000",
			ns.Name(), labelSelector, container)
		if err != nil {
			return fmt.Errorf("unable to get access logs from mixer: %v , content %v", err, content)
		}
		err = validate(content)
		if err != nil {
			return fmt.Errorf("error validating content %v ", err)
		}
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		t.Fatalf("Failed with err: %v", err)
	}
}

func FetchRequestCount(t *testing.T, prometheus prometheus.Instance, service, additionalLabels, namespace string,
	totalReqExpected float64) (prior429s float64, prior200s float64) {
	var err error
	t.Log("Establishing metrics baseline for test...")

	retry := util.Retrier{
		BaseDelay: 30 * time.Second,
		Retries:   2,
	}
	metricName := "istio_requests_total"

	retryFn := func(_ context.Context, i int) error {
		t.Helper()
		t.Logf("Trying to find metrics via promql (attempt %d)...", i)
		query := fmt.Sprintf("istio_requests_total{%s=\"%s\",%s=\"%s\",%s}", destLabel, Fqdn(service, namespace), reporterLabel, "destination", additionalLabels)
		totalReq, err := getMetric(t, prometheus, query, "istio_requests_total")
		t.Logf("Expected Req: %v Got: %v", totalReqExpected, totalReq)
		if totalReqExpected == float64(0) && totalReq == float64(0) {
			return fmt.Errorf("returning 0. totalReqExpected : %v", totalReqExpected)
		}
		if err != nil {
			return err
		}
		if totalReq < totalReqExpected {
			return fmt.Errorf("total Requests: %f less than expected: %f", totalReq, totalReqExpected)
		}
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, PromDump(prometheus, metricName))
		t.Logf("could not find istio_requests_total  (msg: %v)", err)
		return 0, 0
	}

	query := fmt.Sprintf("istio_requests_total{%s=\"%s\",%s=\"%s\",%s=\"%s\",%s}", destLabel, Fqdn(service, namespace),
		reporterLabel, "destination", responseCodeLabel, "429", additionalLabels)
	prior429s, err = getMetric(t, prometheus, query, "istio_requests_total")
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, PromDump(prometheus, metricName))
		t.Logf("error getting prior 429s, using 0 as value (msg: %v)", err)
		prior429s = 0
	}

	query = fmt.Sprintf("istio_requests_total{%s=\"%s\",%s=\"%s\",%s=\"%s\",%s}", destLabel, Fqdn(service, namespace),
		reporterLabel, "destination", responseCodeLabel, "200", additionalLabels)
	prior200s, err = getMetric(t, prometheus, query, "istio_requests_total")
	if err != nil {
		t.Logf("prometheus values for %s:\n%s", metricName, PromDump(prometheus, metricName))
		t.Logf("error getting prior 200s, using 0 as value (msg: %v)", err)
		prior200s = 0
	}
	t.Logf("Baseline established: prior200s = %f, prior429s = %f", prior200s, prior429s)

	return prior429s, prior200s
}

func AllowRuleSync(t *testing.T) {
	t.Log("Sleeping to allow rules to take effect...")
	time.Sleep(15 * time.Second)
}
