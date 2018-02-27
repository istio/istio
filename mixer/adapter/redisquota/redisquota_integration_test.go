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
    rateLimitAlgorithm: FIXED_WINDOW
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

func TestFixedWindowAlgorithm(t *testing.T) {
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
	defer func() {
		_ = mixerServer.Close()
	}()

	// start mixer client
	conn, err := grpc.Dial(mixerServer.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Errorf("Creating client failed: %v", err)
	}
	mixerClient := istio_mixer_v1.NewMixerClient(conn)

	requestBag := attribute.GetMutableBag(nil)
	requestBag.Set(args.ConfigIdentityAttribute, args.ConfigIdentityAttributeDomain)

	attrs := map[string]interface{}{}
	for k, v := range attrs {
		requestBag.Set(k, v)
	}
	var attrProto istio_mixer_v1.CompressedAttributes
	requestBag.ToProto(&attrProto, nil, 0)

	quotas := map[string]istio_mixer_v1.CheckRequest_QuotaParams{
		"key1": {
			Amount:     30,
			BestEffort: true,
		},
	}

	req := &istio_mixer_v1.CheckRequest{
		Attributes: attrProto,
		Quotas:     quotas,
	}

	res, err := mixerClient.Check(context.Background(), req)
	if err != nil {
		t.Errorf("Got error during Check: %v", err)
	}

	if res.Precondition.Status.Code != 0 {
		t.Errorf("Expected status: %v, Got: %v", 0, res.Precondition.Status.Code)
	}

	expected := map[string]int64{
		"key1": 30,
	}

	if len(expected) != len(res.Quotas) {
		t.Errorf("Expected response size: %v, Got: %v", len(expected), len(res.Quotas))
	}

	for name, allocated := range expected {
		if quota, ok := res.Quotas[name]; ok {
			if quota.GrantedAmount != allocated {
				t.Errorf("Expected Grant amount: %v, Got: %v", allocated, quota.GrantedAmount)
			}
		} else {
			t.Errorf("Required quota not allocated: %v", name)
		}
	}
}
