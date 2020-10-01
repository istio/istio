// +build integ
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

package telemetry

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/periodic"

	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/components/prometheus"
)

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

func AllowRuleSync(t *testing.T) {
	t.Log("Sleeping to allow rules to take effect...")
	time.Sleep(15 * time.Second)
}
