// Copyright 2018 Istio Authors
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

package redisquota

import (
	"os"
	"strings"
	"testing"

	"github.com/alicebob/miniredis"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	adapterConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: redisquota
metadata:
  name: handler
  namespace: istio-system
spec:
  quotas:
  - name: requestCount.quota.istio-system
    maxAmount: 50
    validDuration: 30s
    bucketDuration: 1s
    rateLimitAlgorithm: __RATE_LIMIT_ALGORITHM__
    overrides:
    # The following override applies to 'ratings' when
    # the source is 'reviews'.
    - dimensions:
        destination: ratings
        source: reviews
      maxAmount: 12
    # The following override applies to 'ratings' regardless
    # of the source.
    - dimensions:
        destination: reviews
      maxAmount: 5
  # Redis connection pool
  redisServerUrl: __REDIS_SERVER_ADDRESS__
  connectionPoolSize: 10

---

apiVersion: "config.istio.io/v1alpha2"
kind: quota
metadata:
  name: requestCount
  namespace: istio-system
spec:
  dimensions:
    source: source.labels["app"] | source.service | "unknown"
    sourceVersion: source.labels["version"] | "unknown"
    destination: destination.labels["app"] | destination.service | "unknown"
    destinationVersion: destination.labels["version"] | "unknown"

---

apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: quota
  namespace: istio-system
spec:
  actions:
  - handler: handler.redisquota
    instances:
    - requestCount.quota

`
)

func runServerWithSelectedAlgorithm(t *testing.T, algorithm string) {
	cases := map[string]struct {
		attrs  map[string]interface{}
		quotas map[string]istio_mixer_v1.CheckRequest_QuotaParams
		want   string
	}{
		"Request 30 when 50 is available": {
			attrs: map[string]interface{}{},
			quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
				"key1": {
					Amount:     30,
					BestEffort: true,
				},
			},
			want: `
			 {
			  "AdapterState": null,
			  "Returns": [
			   {
			    "Quota": {
			 	"key1": {
			 	 "ValidDuration": 30000000000,
			 	 "Amount": 30
			 	}
			    }
			   }
			  ]
			 }
			`,
		},
		"Exceed allocation request with bestEffort": {
			attrs: map[string]interface{}{},
			quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
				"key2": {
					Amount:     60,
					BestEffort: true,
				},
			},
			want: `
			{
			 "Returns": [
			  {
			   "Quota": {
				"key2": {
				 "ValidDuration": 30000000000,
				 "Amount": 50
				}
			   }
			  }
			 ]
			}
			`,
		},
		"Exceed allocation request without bestEffort": {
			attrs: map[string]interface{}{},
			quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
				"key3": {
					Amount:     60,
					BestEffort: false,
				},
			},
			want: `
			{
			 "Returns": [
			  {
			   "Quota": {
			    "key3": {
			     "ValidDuration": 0,
			     "Amount": 0
			    }
			   }
			  }
			 ]
			}
			`,
		},
		"Dimension override with best effort": {
			attrs: map[string]interface{}{
				"source.service":      "reviews",
				"destination.service": "ratings",
			},
			quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
				"overridden": {
					Amount:     15,
					BestEffort: true,
				},
			},
			want: `
			{
			 "AdapterState": null,
			 "Returns": [
			  {
			   "Quota": {
			    "overridden": {
				 "ValidDuration": 30000000000,
				 "Amount": 12
			    }
			   }
			  }
			 ]
			}
			`,
		},
	}
	// start mock redis server
	for id, c := range cases {
		mockRedis, err := miniredis.Run()
		if err != nil {
			t.Fatalf("Unable to start mock redis server: %v", err)
		}
		defer mockRedis.Close()
		serviceCfg := adapterConfig
		serviceCfg = strings.Replace(serviceCfg, "__RATE_LIMIT_ALGORITHM__", algorithm, -1)
		serviceCfg = strings.Replace(serviceCfg, "__REDIS_SERVER_ADDRESS__", mockRedis.Addr(), -1)

		t.Logf("**Executing test case '%s'**", id)
		adapter_integration.RunTest(
			t,
			GetInfo,
			adapter_integration.Scenario{
				ParallelCalls: []adapter_integration.Call{
					{
						CallKind: adapter_integration.CHECK,
						Attrs:    c.attrs,
						Quotas:   c.quotas,
					},
				},

				Setup: func() (interface{}, error) {
					mockRedis, err := miniredis.Run()
					if err != nil {
						t.Fatalf("Unable to start mock redis server: %v", err)
					}
					return mockRedis, nil
				},

				Teardown: func(ctx interface{}) {
					ctx.(*miniredis.Miniredis).Close()
				},

				Configs: []string{
					serviceCfg,
				},
				Want: c.want,
			},
		)
	}
}

func TestFixedWindowAlgorithm(t *testing.T) {
	if os.Getenv("RACE_TEST") == "true" {
		t.Skip("Test fails in race testing, being fixed in issue #3789")
	}
	runServerWithSelectedAlgorithm(t, "ROLLING_WINDOW")
}

func TestRollingWindowAlgorithm(t *testing.T) {
	runServerWithSelectedAlgorithm(t, "FIXED_WINDOW")
}
