// Copyright 2016 Google Inc.
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

package ipListChecker

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gopkg.in/yaml.v2"

	pb "istio.io/mixer/adapter/ipListChecker/config"
	"istio.io/mixer/pkg/adaptertesting"
	"istio.io/mixer/pkg/aspect"
)

// TODO: this test suite needs to be beefed up considerably.
// Should be testing more edge cases, testing refresh behavior with and without errors,
// testing TTL handling, testing malformed input, etc.

type env struct{}

func (e *env) Logger() aspect.Logger { return &logger{} }

type logger struct{}

func (l *logger) Infof(format string, args ...interface{})        {}
func (l *logger) Warningf(format string, args ...interface{})     {}
func (l *logger) Errorf(format string, args ...interface{}) error { return fmt.Errorf(format, args) }

func TestBasic(t *testing.T) {
	lp := listPayload{
		WhiteList: []string{"10.10.11.2", "10.10.11.3", "9.9.9.9/28"},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := yaml.Marshal(lp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(out); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()
	b := newAdapter()

	config := pb.Config{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1,
		Ttl:             10,
	}

	a, err := b.NewAspect(&env{}, &config)
	if err != nil {
		t.Errorf("Unable to create adapter: %v", err)
	}
	cases := []string{"10.10.11.2", "9.9.9.1"}
	for _, c := range cases {
		ok, err := a.CheckList(c)
		if err != nil {
			t.Errorf("CheckList(%s) failed with %v", c, err)
		}

		if !ok {
			t.Errorf("CheckList(%s): expecting 'true', got 'false'", c)
		}
	}

	negCases := []string{"120.10.11.2"}
	for _, c := range negCases {
		ok, err := a.CheckList(c)
		if err != nil {
			t.Errorf("CheckList(%s) failed with %v", c, err)
		}

		if ok {
			t.Errorf("CheckList(%s): expecting 'false', got 'true'", c)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	type testCase struct {
		cfg   pb.Config
		field string
	}

	cases := []testCase{
		{
			cfg:   pb.Config{ProviderUrl: "Foo", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},

		{
			cfg:   pb.Config{ProviderUrl: ":", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},

		{
			cfg:   pb.Config{ProviderUrl: "http:", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},

		{
			cfg:   pb.Config{ProviderUrl: "http://", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},

		{
			cfg:   pb.Config{ProviderUrl: "http:///FOO", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},
	}

	b := newAdapter()
	for i, c := range cases {
		err := b.ValidateConfig(&c.cfg).Multi.Errors[0].(aspect.ConfigError)
		if err.Field != c.field {
			t.Errorf("Case %d: expecting error for field %s, got %s", i, c.field, err.Field)
		}
	}
}

func TestInvariants(t *testing.T) {
	adaptertesting.TestAdapterInvariants(newAdapter(), Register, t)
}
