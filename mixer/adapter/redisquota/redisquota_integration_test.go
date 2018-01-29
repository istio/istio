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
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis"
	"google.golang.org/grpc"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/mixer/template"
)

const (
	globalConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
  attributes:
    source.service:
      value_type: STRING
    destination.service:
      value_type: STRING
    source.labels:
      valueType: STRING_MAP
    destination.labels:
      valueType: STRING_MAP
`
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
		attrs      map[string]interface{}
		quotas     map[string]istio_mixer_v1.CheckRequest_QuotaParams
		statusCode int32
		expected   map[string]int64
	}{
		"Request 30 when 50 is available": {
			attrs: map[string]interface{}{},
			quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
				"key1": {
					Amount:     30,
					BestEffort: true,
				},
			},
			expected: map[string]int64{
				"key1": 30,
			},
			statusCode: 0,
		},
		"Exceed allocation request with bestEffort": {
			attrs: map[string]interface{}{},
			quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
				"key2": {
					Amount:     60,
					BestEffort: true,
				},
			},
			expected: map[string]int64{
				"key2": 50,
			},
			statusCode: 0,
		},
		"Exceed allocation request without bestEffort": {
			attrs: map[string]interface{}{},
			quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
				"key3": {
					Amount:     60,
					BestEffort: false,
				},
			},
			expected: map[string]int64{
				"key3": 0,
			},
			statusCode: 0,
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
			expected: map[string]int64{
				"overridden": 12,
			},
			statusCode: 0,
		},
	}

	for id, c := range cases {
		// start mock redis server
		mockRedis, err := miniredis.Run()
		if err != nil {
			t.Fatalf("Unable to start mock redis server: %v", err)
		}
		defer mockRedis.Close()

		// start mixer with redisquota adapter
		args := server.NewArgs()

		args.APIPort = 0
		args.MonitoringPort = 0
		args.Templates = template.SupportedTmplInfo
		args.Adapters = []adapter.InfoFn{
			GetInfo,
		}

		serviceCfg := adapterConfig
		serviceCfg = strings.Replace(serviceCfg, "__RATE_LIMIT_ALGORITHM__", algorithm, -1)
		serviceCfg = strings.Replace(serviceCfg, "__REDIS_SERVER_ADDRESS__", mockRedis.Addr(), -1)
		var cerr error
		if args.ConfigStore, cerr = storetest.SetupStoreForTest(globalConfig, serviceCfg); cerr != nil {
			t.Fatal(cerr)
		}

		mixerServer, err := server.New(args)
		if err != nil {
			t.Fatalf("Unable to create server: %v", err)
		}
		mixerServer.Run()

		// start mixer client
		conn, err := grpc.Dial(mixerServer.Addr().String(), grpc.WithInsecure())
		if err != nil {
			t.Errorf("Creating client failed: %v", err)
		}
		mixerClient := istio_mixer_v1.NewMixerClient(conn)

		requestBag := attribute.GetMutableBag(nil)
		requestBag.Set(args.ConfigIdentityAttribute, args.ConfigIdentityAttributeDomain)

		for k, v := range c.attrs {
			requestBag.Set(k, v)
		}
		var attrProto istio_mixer_v1.CompressedAttributes
		requestBag.ToProto(&attrProto, nil, 0)

		req := &istio_mixer_v1.CheckRequest{
			Attributes: attrProto,
			Quotas:     c.quotas,
		}

		res, err := mixerClient.Check(context.Background(), req)
		if err != nil {
			t.Errorf("%v: Got error during Check: %v", id, err)
		}

		if res.Precondition.Status.Code != c.statusCode {
			t.Errorf("%v: Expected status: %v, Got: %v", id, c.statusCode, res.Precondition.Status.Code)
		}

		if len(c.expected) != len(res.Quotas) {
			t.Errorf("%v: Expected response size: %v, Got: %v", id, len(c.expected), len(res.Quotas))
		}

		for name, allocated := range c.expected {
			if quota, ok := res.Quotas[name]; ok {
				if quota.GrantedAmount != allocated {
					t.Errorf("%v: Expected Grant amount: %v, Got: %v", id, allocated, quota.GrantedAmount)
				}
			} else {
				t.Errorf("%v: Required quota not allocated: %v", id, name)
			}
		}
	}
}

func TestFixedWindowAlgorithm(t *testing.T) {
	runServerWithSelectedAlgorithm(t, "ROLLING_WINDOW")
	runServerWithSelectedAlgorithm(t, "FIXED_WINDOW")
}
