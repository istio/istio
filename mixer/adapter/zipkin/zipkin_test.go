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

package zipkin

import (
	"context"
	"strings"
	"testing"

	"istio.io/istio/mixer/adapter/zipkin/config"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/tracespan"
)

func TestBuild(t *testing.T) {
	ctx := context.Background()
	info := GetInfo()
	b := info.NewBuilder().(tracespan.HandlerBuilder)
	b.SetAdapterConfig(&config.Params{
		Url:               "http://127.0.0.1:9999",
		SampleProbability: 1.0,
	})
	b.SetTraceSpanTypes(make(map[string]*tracespan.Type))
	handler, err := b.Build(ctx, test.NewEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	_ = handler.(tracespan.Handler)
	handler.Close()
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Params
		wantErr string
	}{
		{
			name: "valid zero probability",
			cfg: &config.Params{
				SampleProbability: 0.0,
				Url:               "http://localhost:9999",
			},
		},
		{
			name: "valid sample 1.0",
			cfg: &config.Params{
				SampleProbability: 1.0,
				Url:               "http://localhost:9999",
			},
		},
		{
			name: "invalid SampleProbability",
			cfg: &config.Params{
				Url:               "http://localhost:9999",
				SampleProbability: 50.0,
			},
			wantErr: "must be between 0 and 1 inclusive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := GetInfo().NewBuilder().(tracespan.HandlerBuilder)
			b.SetAdapterConfig(tt.cfg)
			err := b.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatal("Unexpected error", err)
			}
			if tt.wantErr == "" {
				return
			}
			if got, want := err.Error(), tt.wantErr; !strings.Contains(got, want) {
				t.Fatalf("v.Validate() = %s; want %s", got, want)
			}
		})
	}
}
