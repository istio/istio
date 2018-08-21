// Copyright 2018 the Istio Authors.
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
	"testing"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
)

func TestGetStackdriverExporter(t *testing.T) {
	ctx := context.Background()
	var params config.Params
	params.Trace = &config.Params_Trace{SampleProbability: 1}
	params.ProjectId = "example-project"
	params.Creds = &config.Params_ServiceAccountPath{
		ServiceAccountPath: "testdata/serviceaccount.json",
	}
	_, err := getStackdriverExporter(ctx, &testEnv{}, &params)
	if err != nil {
		t.Fatal(err)
	}

	// Try to call getStackdriverExporter again with the same project ID: this should work.
	_, err = getStackdriverExporter(ctx, &testEnv{}, &params)
	if err != nil {
		t.Fatal(err)
	}
}

type testEnv struct {
	data.FakeEnv
}
