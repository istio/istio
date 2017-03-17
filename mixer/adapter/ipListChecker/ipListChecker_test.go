// Copyright 2016 Istio Authors
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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	ptypes "github.com/gogo/protobuf/types"

	"istio.io/mixer/adapter/ipListChecker/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
)

type junk struct{}

func (junk) Read(p []byte) (n int, err error) {
	return 0, errors.New("nothing good ever happens to me")
}

func TestBasic(t *testing.T) {
	list0 := listPayload{
		WhiteList: []string{"10.10.11.2", "10.10.11.3", "9.9.9.9/28"},
	}

	list1 := listPayload{
		WhiteList: []string{"10.10.11.2", "X", "10.10.11.3"},
	}

	list2 := []string{"JUNK"}

	listToUse := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out []byte
		var err error

		if listToUse == 0 {
			out, err = yaml.Marshal(list0)
		} else if listToUse == 1 {
			out, err = yaml.Marshal(list1)
		} else {
			out, err = yaml.Marshal(list2)
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

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: toDuration(1),
		Ttl:             toDuration(10),
	}

	a, err := b.NewListsAspect(test.NewEnv(t), &cfg)
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
			t.Errorf("CheckList(%s): got '%v', expecting '%v'", c.addr, ok, c.result)
		}
	}

	lc := a.(*listChecker)

	// do a NOP refresh of the same data
	lc.fetchList()
	ls := lc.getListState()
	if ls.fetchError != nil {
		t.Errorf("Unable to refresh list: %v", ls.fetchError)
	}

	// now try to parse a list with errors
	listToUse = 1
	lc.fetchList()
	ls = lc.getListState()
	if ls.fetchError != nil {
		t.Errorf("Unable to refresh list: %v", ls.fetchError)
	}
	if len(ls.entries) != 2 {
		t.Errorf("Got %d entries, expected %d", len(ls.entries), len(list1.WhiteList)-1)
	}

	// now try to parse a list in the wrong format
	listToUse = 2
	lc.fetchList()
	ls = lc.getListState()
	if ls.fetchError == nil {
		t.Error("Got success, expected error")
	}
	if len(ls.entries) != 2 {
		t.Errorf("Got %d entries, expected %d", len(ls.entries), len(list1.WhiteList)-1)
	}

	// now try to process an incorrect body
	lc.fetchListWithBody(junk{})
	ls = lc.getListState()
	if ls.fetchError == nil {
		t.Error("Got success, expected failure")
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

	cfg := config.Params{
		ProviderUrl:     "http://abadurl.com",
		RefreshInterval: toDuration(1),
		Ttl:             toDuration(10),
	}

	a, err := b.NewListsAspect(test.NewEnv(t), &cfg)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	if _, err = a.CheckList("1.2.3.4"); err == nil {
		t.Error("Got success, expected failure")
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
			cfg:   config.Params{ProviderUrl: "Foo", RefreshInterval: toDuration(1), Ttl: toDuration(2)},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: ":", RefreshInterval: toDuration(1), Ttl: toDuration(2)},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http:", RefreshInterval: toDuration(1), Ttl: toDuration(2)},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://", RefreshInterval: toDuration(1), Ttl: toDuration(2)},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http:///FOO", RefreshInterval: toDuration(1), Ttl: toDuration(2)},
			field: "ProviderUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://foo.com", RefreshInterval: toDuration(0), Ttl: toDuration(2)},
			field: "RefreshInterval",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://foo.com", RefreshInterval: &ptypes.Duration{Seconds: -1, Nanos: -1}, Ttl: toDuration(2)},
			field: "RefreshInterval",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://foo.com", RefreshInterval: &ptypes.Duration{Seconds: 0x7fffffffffffffff, Nanos: -1}, Ttl: toDuration(2)},
			field: "RefreshInterval",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://foo.com", RefreshInterval: toDuration(1), Ttl: &ptypes.Duration{Seconds: 0x7fffffffffffffff, Nanos: -1}},
			field: "Ttl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://foo.com", RefreshInterval: toDuration(1), Ttl: toDuration(0)},
			field: "Ttl",
		},
	}

	b := newBuilder()
	for i, c := range cases {
		err := b.ValidateConfig(&c.cfg).Multi.Errors[0].(adapter.ConfigError)
		if err.Field != c.field {
			t.Errorf("Case %d: got field %s, expected field %s", i, err.Field, c.field)
		}
	}
}

func TestRefreshAndPurge(t *testing.T) {
	list := listPayload{
		WhiteList: []string{"10.10.11.2", "10.10.11.3", "9.9.9.9/28"},
	}

	blocker := make(chan bool, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out []byte
		var err error

		out, err = yaml.Marshal(list)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(out); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}

		<-blocker
	}))
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: toDuration(3600),
		Ttl:             toDuration(360000),
	}

	refreshChan := make(chan time.Time)
	purgeChan := make(chan time.Time, 1)

	refreshTicker := time.NewTicker(time.Second * 3600)
	purgeTimer := time.NewTimer(time.Second * 3600)

	refreshTicker.C = refreshChan
	purgeTimer.C = purgeChan

	// allow the initial synchronous fetch to go through
	blocker <- true

	a, err := newListCheckerWithTimers(test.NewEnv(t), &cfg, refreshTicker, purgeTimer, time.Second*3600)
	if err != nil {
		t.Errorf("Unable to create aspect: %v", err)
	}

	// cause a redundant refresh
	refreshChan <- time.Now()
	blocker <- true

	// cause a purge
	purgeChan <- time.Now()
	for len(a.getListState().entries) != 0 {
		time.Sleep(time.Millisecond)
	}

	// start a new fetch
	refreshChan <- time.Now()

	// while the fetch is blocked, prime the purge channel. We want to exercise the
	// purge cancellation logic
	purgeChan <- time.Now()

	// unblock the fetch
	blocker <- true

	// start another fetch so we can synchronize
	refreshChan <- time.Now()

	// while the fetch is blocked, make sure the list wasn't purged
	if len(a.getListState().entries) == 0 {
		t.Error("Got an empty list, expecting some entries")
	}

	// allow the fetch to complete
	blocker <- true

	if err := a.Close(); err != nil {
		t.Errorf("Unable to close aspect: %v", err)
	}
}

func TestInvariants(t *testing.T) {
	test.AdapterInvariants(Register, t)
}

func toDuration(seconds int32) *ptypes.Duration {
	return ptypes.DurationProto(time.Duration(seconds) * time.Second)
}
