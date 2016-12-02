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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gopkg.in/yaml.v2"
)

// TODO: this test suite needs to be beefed up considerably.
// Should be testing more edge cases, testing refresh behavior with and without errors,
// testing TTL handling, testing malformed input, etc.

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
		w.Write(out)
	}))
	defer ts.Close()

	config := AdapterConfig{
		ProviderURL:     ts.URL,
		RefreshInterval: time.Second,
		TimeToLive:      time.Second * 10,
	}

	a, err := newAdapter(&config)
	if err != nil {
		t.Error("unable to create adapter " + err.Error())
	}

	var ok bool
	ok, err = a.CheckList("10.10.11.2")
	if !ok {
		t.Error("Expecting check to pass")
	}

	ok, err = a.CheckList("9.9.9.1")
	if !ok {
		t.Error("Expecting check to pass")
	}

	ok, err = a.CheckList("120.10.11.2")
	if ok {
		t.Error("Expecting check to fail")
	}
}
