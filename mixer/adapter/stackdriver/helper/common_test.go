// Copyright 2017 the Istio Authors.
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

package helper

import (
	"fmt"
	"reflect"
	"testing"

	gapiopts "google.golang.org/api/option"

	"istio.io/mixer/adapter/stackdriver/config"
)

func TestToOpts(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Params
		out  []gapiopts.ClientOption // we only assert that the types match, so contents of the option don't matter
	}{
		{"empty", &config.Params{}, []gapiopts.ClientOption{}},
		{"api key", &config.Params{Creds: &config.Params_ApiKey{}}, []gapiopts.ClientOption{gapiopts.WithAPIKey("")}},
		{"app creds", &config.Params{Creds: &config.Params_AppCredentials{}}, []gapiopts.ClientOption{}},
		{"service account",
			&config.Params{Creds: &config.Params_ServiceAccountPath{}},
			[]gapiopts.ClientOption{gapiopts.WithServiceAccountFile("")}},
		{"endpoint",
			&config.Params{Endpoint: "foo.bar"},
			[]gapiopts.ClientOption{gapiopts.WithEndpoint("")}},
		{"endpoint + svc account",
			&config.Params{Endpoint: "foo.bar", Creds: &config.Params_ServiceAccountPath{}},
			[]gapiopts.ClientOption{gapiopts.WithEndpoint(""), gapiopts.WithServiceAccountFile("")}},
	}
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			opts := ToOpts(tt.cfg)
			if len(opts) != len(tt.out) {
				t.Errorf("len(toOpts(%v)) = %d, expected %d", tt.cfg, len(opts), len(tt.out))
			}

			optSet := make(map[gapiopts.ClientOption]struct{})
			for _, opt := range opts {
				optSet[opt] = struct{}{}
			}

			for _, expected := range tt.out {
				found := false
				for _, actual := range opts {
					// We care that the types are what we expect, not necessarily that they're identical
					found = found || (reflect.TypeOf(expected) == reflect.TypeOf(actual))
				}
				if !found {
					t.Errorf("toOpts() = %v, wanted opt '%v' (type %v)", opts, expected, reflect.TypeOf(expected))
				}
			}
		})
	}
}
