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
	"errors"
	"fmt"
	"reflect"
	"testing"

	gapiopts "google.golang.org/api/option"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/pkg/adapter"
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
			[]gapiopts.ClientOption{gapiopts.WithCredentialsFile("")}},
		{"endpoint",
			&config.Params{Endpoint: "foo.bar"},
			[]gapiopts.ClientOption{gapiopts.WithEndpoint("")}},
		{"endpoint + svc account",
			&config.Params{Endpoint: "foo.bar", Creds: &config.Params_ServiceAccountPath{}},
			[]gapiopts.ClientOption{gapiopts.WithEndpoint(""), gapiopts.WithCredentialsFile("")}},
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

func TestFillProjectID(t *testing.T) {
	tests := []struct {
		name       string
		shouldFill shouldFillFn
		projectID  projectIDFn
		in         adapter.Config
		res        string
	}{
		{"has project id", func(*config.Params) bool { return false }, func() (string, error) { return "", nil }, &config.Params{ProjectId: "id"}, "id"},
		{"should not fill", func(*config.Params) bool { return false }, func() (string, error) { return "", nil }, &config.Params{}, ""},
		{"fail to get project id", func(*config.Params) bool { return true }, func() (string, error) { return "", errors.New("error") }, &config.Params{}, ""},
		{"get project id", func(*config.Params) bool { return true }, func() (string, error) { return "id", nil }, &config.Params{}, "id"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			fp := NewProjectIDFiller(tt.shouldFill, tt.projectID)
			fp.FillProjectID(tt.in)
			cfg := tt.in.(*config.Params)
			if cfg.ProjectId != tt.res {
				t.Errorf("project ID %v is wrong, expected project ID %v.", cfg.ProjectId, tt.res)
			}
		})
	}
}
