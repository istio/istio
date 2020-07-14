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

	"go.opencensus.io/trace"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/adapter/stackdriver/helper"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
)

var dummyShouldFill = func() bool { return true }
var dummyMetadataFn = func() (string, error) { return "", nil }

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		params  *config.Params_Trace
		wantErr bool
	}{
		{"zero", &config.Params_Trace{SampleProbability: 0}, false},
		{"10percent", &config.Params_Trace{SampleProbability: 0.1}, false},
		{"negative", &config.Params_Trace{SampleProbability: -1}, true},
		{"too big", &config.Params_Trace{SampleProbability: 100}, true},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			var (
				b builder
			)
			b.SetAdapterConfig(&config.Params{Trace: tt.params})
			err := b.Validate()
			if !tt.wantErr && err != nil {
				t.Errorf("wantErr = false, got %s", err)
			}
			if tt.wantErr && err == nil {
				t.Errorf("wantErr, got nil")
			}
		})
	}
}

func TestProjectID(t *testing.T) {
	getExporterFunc = func(_ context.Context, _ adapter.Env, params *config.Params) (trace.Exporter, error) {
		if params.ProjectId != "pid" {
			return nil, fmt.Errorf("wanted pid got %v", params.ProjectId)
		}
		return nil, nil
	}

	tests := []struct {
		name string
		cfg  *config.Params
		pid  func() (string, error)
	}{
		{
			"empty project id",
			&config.Params{
				ProjectId: "",
			},
			func() (string, error) { return "pid", nil },
		},
		{
			"filled project id",
			&config.Params{
				ProjectId: "pid",
			},
			func() (string, error) { return "meta-pid", nil },
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			mg := helper.NewMetadataGenerator(dummyShouldFill, tt.pid, dummyMetadataFn, dummyMetadataFn)
			b := &builder{mg: mg}
			b.SetAdapterConfig(tt.cfg)
			_, err := b.Build(context.Background(), test.NewEnv(t))
			if err != nil {
				t.Errorf("Project id is not expected: %v", err)
			}
		})
	}
}
