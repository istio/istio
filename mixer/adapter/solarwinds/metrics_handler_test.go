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

package solarwinds

import (
	"context"
	"testing"
	"time"

	"istio.io/istio/mixer/adapter/solarwinds/config"
	test2 "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/metric"
)

func TestNewMetricsHandler(t *testing.T) {
	env := test2.NewEnv(t)
	logger := env.Logger()
	type testData struct {
		name string
		cfg  *config.Params
	}
	tests := []testData{
		{
			name: "All good",
			cfg: &config.Params{
				AppopticsAccessToken: "asdfsdf",
			},
		},
		{
			name: "No access token",
			cfg:  &config.Params{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger.Infof("Starting %s - test run. . .\n", t.Name())
			defer logger.Infof("Finished %s - test run. . .", t.Name())
			mhi := newMetricsHandler(test2.NewEnv(t), test.cfg)
			defer mhi.close()

			mh, ok := mhi.(*metricsHandler)
			if !ok || mh == nil {
				t.Errorf("Instance is not of valid type")
			}
		})
	}
}

func TestHandleMetric(t *testing.T) {
	ctx := context.Background()
	env := test2.NewEnv(t)
	logger := env.Logger()

	logger.Infof("Starting %s - test run. . .\n", t.Name())
	defer logger.Infof("Finished %s - test run. . .", t.Name())

	mhi := newMetricsHandler(test2.NewEnv(t), &config.Params{
		Metrics: map[string]*config.Params_MetricInfo{
			"m1": {
				LabelNames: []string{"tag1"},
			},
			"m2": {
				LabelNames: []string{"tag2"},
			},
		},
	})
	defer mhi.close()
	err := mhi.handleMetric(ctx, []*metric.Instance{
		{
			Name:  "m1",
			Value: 1, // int
			Dimensions: map[string]interface{}{
				"tag1": 1,
			},
		},
		{
			Name:  "m2",
			Value: 3.4, // float
			Dimensions: map[string]interface{}{
				"tag2": 3.4,
			},
		},
		{
			Name:  "m3",
			Value: 5 * time.Second, // duration
			Dimensions: map[string]interface{}{
				"tag3": "hello",
			},
		},
		{
			Name:  "m3",
			Value: "abc", // string
		},
	})

	if err != nil {
		t.Errorf("Unexpected error while running %s test - %v", t.Name(), err)
		return
	}
}
