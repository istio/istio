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

package trace

import (
	"context"
	"fmt"
	"testing"
	"time"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	testenv "istio.io/istio/mixer/pkg/adapter/test"
)

const (
	projectID          = "example-project"
	serviceAccountPath = "testdata/serviceaccount.json"
	sampleProbability  = 1
)

func TestGetStackdriverExporter(t *testing.T) {
	ctx := context.Background()
	params := newTestParams()
	_, err := getStackdriverExporter(ctx, testenv.NewEnv(t), params)
	if err != nil {
		t.Fatal(err)
	}

	// Try to call getStackdriverExporter again with the same project ID: this should work.
	_, err = getStackdriverExporter(ctx, testenv.NewEnv(t), params)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetOptions_flushInterval(t *testing.T) {
	type duration struct {
		flushInterval time.Duration
	}

	tests := []struct {
		name         string
		duration     *duration
		wantInterval time.Duration
	}{
		{"unspecified", nil, 0 * time.Second},                      // default
		{"negative", &duration{-1 * time.Second}, 0 * time.Second}, // default
		{"positive", &duration{2 * time.Second}, 2 * time.Second},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			params := newTestParams()
			if tt.duration != nil {
				params.PushInterval = tt.duration.flushInterval
			}

			opts := getOptions(testenv.NewEnv(t), params)
			delayInterval := opts.BundleDelayThreshold
			if delayInterval != tt.wantInterval {
				t.Fatalf("wanted delayInterval %s; got %s", tt.wantInterval, delayInterval)
			}
		})
	}
}

// newTestParams returns a pointer to a new default instance of config.Params
// that can be used for testing.
func newTestParams() *config.Params {
	return &config.Params{
		ProjectId: projectID,
		Creds: &config.Params_ServiceAccountPath{
			ServiceAccountPath: serviceAccountPath,
		},
		Trace: &config.Params_Trace{SampleProbability: sampleProbability},
	}
}
