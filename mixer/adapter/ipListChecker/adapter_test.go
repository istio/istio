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

	"istio.io/mixer/pkg/aspect"
)

// TODO: this test suite needs to be beefed up considerably.
// Should be testing more edge cases, testing refresh behavior with and without errors,
// testing TTL handling, testing malformed input, etc.

type env struct {}
func (e *env) Logger() aspect.Logger {return &logger{} }

type logger struct {}
func (l *logger) Infof(format string, args ...interface{}) {}
func (l *logger) Warningf(format string, args ...interface{}) {}
func (l *logger) Errorf(format string, args ...interface{}) error {return fmt.Errorf(format, args)}

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
	if err := b.ValidateConfig(b.DefaultConfig()); err != nil {
		t.Errorf("Failed validation %#v", err)
	}

	config := Config{
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
