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

package signalfx

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	sfxproto "github.com/signalfx/com_signalfx_metrics_protobuf"

	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	requestCountToSignalFxCfg = `
apiVersion: "config.istio.io/v1alpha2"
kind: signalfx
metadata:
  name: handler
  namespace: istio-system
spec:
  access_token: abcdef
  ingest_url: %s
  metrics:
  - name: requestcount.metric.istio-system
    type: COUNTER
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  actions:
  - handler: handler.signalfx
    instances:
    - requestcount.metric
---
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
  name: requestcount
  namespace: istio-system
spec:
  value: "1"
  dimensions:
    destination_service: "\"myservice\""
    response_code: "200"
`
)

type fakeSfxIngest struct {
	*httptest.Server
	DPs chan *sfxproto.DataPoint
}

func (f *fakeSfxIngest) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	contents, _ := ioutil.ReadAll(req.Body)
	defer req.Body.Close()
	rw.WriteHeader(http.StatusOK)
	if n, err := io.WriteString(rw, "\"OK\""); err != nil || n != 4 {
		panic("could not write response back to test client")
	}

	dpUpload := &sfxproto.DataPointUploadMessage{}
	err := proto.Unmarshal(contents, dpUpload)
	if err == nil {
		for i := range dpUpload.Datapoints {
			f.DPs <- dpUpload.Datapoints[i]
		}
	}
}

func TestReportMetrics(t *testing.T) {
	fakeIngest := &fakeSfxIngest{
		DPs: make(chan *sfxproto.DataPoint),
	}
	server := httptest.NewServer(fakeIngest)

	adapter_integration.RunTest(
		t,
		GetInfo,
		adapter_integration.Scenario{
			ParallelCalls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
				},
				{
					CallKind: adapter_integration.REPORT,
				},
			},

			GetState: func(_ interface{}) (interface{}, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				var dps []*sfxproto.DataPoint
				for {
					select {
					case <-ctx.Done():
						cancel()
						return dps, nil
					case dp := <-fakeIngest.DPs:
						// Remove timestamp since it is difficult to match
						// against
						dp.Timestamp = nil
						// Dimensions are a slice of random order so make them
						// predictable
						sort.Slice(dp.Dimensions, func(i, j int) bool {
							return *dp.Dimensions[i].Key <= *dp.Dimensions[j].Key
						})

						dps = append(dps, dp)
						cancel()
						return dps, nil
					}
				}
			},

			Configs: []string{
				fmt.Sprintf(requestCountToSignalFxCfg, server.URL),
			},

			Want: `
            {
             "AdapterState": [
               {
                 "dimensions": [
                   {
                     "key": "destination_service",
                     "value": "myservice"
                   },
                   {
                    "key": "response_code",
                    "value": "200"
                   }
                 ],
                 "metric": "requestcount.metric.istio-system",
                 "metricType": 3,
                 "value": {
                   "intValue": 2
                 }
               }
             ],
             "Returns": [
              {
               "Check": {
                "Status": {},
                "ValidDuration": 0,
                "ValidUseCount": 0
               },
               "Error": null,
               "Quota": null
              },
              {
               "Check": {
                "Status": {},
                "ValidDuration": 0,
                "ValidUseCount": 0
               },
               "Error": null,
               "Quota": null
              }
             ]
             }`,
		},
	)
}
