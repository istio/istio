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

package status

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
)

func TestStatsURL(t *testing.T) {
	cases := []struct {
		name             string
		url              string
		envoy            string
		app              string
		output           string
		expectParseError bool
	}{
		{
			name: "envoy stats",
			url:  "/stats/prometheus",
			envoy: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
			output: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
		},
		{
			name: "envoy stats with usedonly",
			url:  "/stats/prometheus?usedonly",
			envoy: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
			output: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
		},
		{
			name: "envoy stats with usedonly",
			url:  "/stats/prometheus?usedonly&filter=.*httpbin2.*",
			envoy: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
			output: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
		},
		{
			name: "envoy stats with filter",
			url:  "/stats/prometheus?filter=.*httpbin2.*",
			envoy: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
			output: `# TYPE my_metric counter
my_metric{} 0
# TYPE my_other_metric counter
my_other_metric{} 0
`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			envoy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != tt.url {
					t.Errorf("expect: %s, actual: %s", tt.url, r.URL.RequestURI())
				}
				if _, err := w.Write([]byte(tt.envoy)); err != nil {
					t.Fatalf("write failed: %v", err)
				}
			}))

			defer envoy.Close()
			app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(tt.app)); err != nil {
					t.Fatalf("write failed: %v", err)
				}
			}))
			defer app.Close()

			envoyPort, err := strconv.Atoi(strings.Split(envoy.URL, ":")[2])
			if err != nil {
				t.Fatal(err)
			}
			server := &Server{
				prometheus: &PrometheusScrapeConfiguration{
					Port: strings.Split(app.URL, ":")[2],
				},
				envoyStatsPort: envoyPort,
				http:           envoy.Client(),
				registry:       TestingRegistry(t),
			}

			req, err := http.NewRequest("GET", tt.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			server.handleStats(rec, req)
			if rec.Code != 200 {
				t.Fatalf("handleStats() => %v; want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.output) {
				t.Fatalf("handleStats() => %v; want %v", rec.Body.String(), tt.output)
			}

			parser := expfmt.TextParser{}
			mfMap, err := parser.TextToMetricFamilies(strings.NewReader(rec.Body.String()))
			if err != nil && !tt.expectParseError {
				t.Fatalf("failed to parse metrics: %v", err)
			} else if err == nil && tt.expectParseError {
				t.Fatalf("expected a prse error, got %+v", mfMap)
			}
		})
	}
}
