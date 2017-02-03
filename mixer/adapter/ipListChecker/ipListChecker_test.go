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

	"istio.io/mixer/adapter/ipListChecker/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

// TODO: this test suite needs to be beefed up considerably.
// Should be testing more edge cases, testing refresh behavior with and without errors,
// testing TTL handling, testing malformed input, etc.

type env struct{}

func (e env) Logger() adapter.Logger { return logger{} }

type logger struct{}

func (l logger) Infof(format string, args ...interface{})        {}
func (l logger) Warningf(format string, args ...interface{})     {}
func (l logger) Errorf(format string, args ...interface{}) error { return fmt.Errorf(format, args) }

func TestBasic(t *testing.T) {
	goodList := listPayload{
		WhiteList: []string{"10.10.11.2", "10.10.11.3", "9.9.9.9/28"},
	}

	badList := listPayload{
		WhiteList: []string{"10.10.11.2", "X", "10.10.11.3"},
	}

	useGoodList := true

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out []byte
		var err error

		if useGoodList {
			out, err = yaml.Marshal(goodList)
		} else {
			out, err = yaml.Marshal(badList)
		}

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(out); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()
	b := newBuilder()

	config := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1,
		Ttl:             10,
	}

	a, err := b.NewListsAspect(&env{}, &config)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	cases := []struct {
		addr   string
		result bool
		fail   bool
	}{
		{"10.10.11.2", true, false},
		{"9.9.9.1", true, false},
		{"120.10.11.2", false, false},
		{"XYZ", false, true},
	}

	for _, c := range cases {
		ok, err := a.CheckList(c.addr)
		if (err != nil) != c.fail {
			t.Errorf("CheckList(%s): did not expect err '%v'", c.addr, err)
		}

		if ok != c.result {
			t.Errorf("CheckList(%s): expecting '%v', got '%v'", c.addr, c.result, ok)
		}
	}

	lc := a.(*listChecker)

	// do a NOP refresh of the same data
	lc.refreshList()

	// now try to parse the bad list
	useGoodList = false
	lc.refreshList()
	list := lc.getList()
	if len(list.payload) != 2 {
		t.Errorf("Expecting %d, got %d entries", len(badList.WhiteList)-1, len(list.payload))
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Unable to close builder: %v", err)
	}
}

func TestBadUrl(t *testing.T) {
	b := newBuilder()

	config := config.Params{
		ProviderUrl:     "http://abadurl.com",
		RefreshInterval: 1,
		Ttl:             10,
	}

	a, err := b.NewListsAspect(&env{}, &config)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	if _, err = a.CheckList("1.2.3.4"); err == nil {
		t.Errorf("Expecting failure, got success")
	}

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Errorf("Unable to close builder: %v", err)
	}
}

func TestValidateConfig(t *testing.T) {
	type testCase struct {
		cfg   config.Params
		field string
	}

	cases := []testCase{
		{
			cfg:   config.Params{ProviderUrl: "Foo", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: ":", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http:", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http:///FOO", RefreshInterval: 1, Ttl: 2},
			field: "ProviderUrl",
		},
	}

	b := newBuilder()
	for i, c := range cases {
		err := b.ValidateConfig(&c.cfg).Multi.Errors[0].(adapter.ConfigError)
		if err.Field != c.field {
			t.Errorf("Case %d: expecting error for field %s, got %s", i, c.field, err.Field)
		}
	}
}

func TestInvariants(t *testing.T) {
	test.AdapterInvariants(Register, t)
}
