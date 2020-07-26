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

package list

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghodss/yaml"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/mixer/adapter/list/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/listentry"
)

func TestBasic(t *testing.T) {
	info := GetInfo()

	if !containsListEntryTemplate(info.SupportedTemplates) {
		t.Error("Didn't find all expected supported templates")
	}

	cfg := info.DefaultConfig
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(cfg)
	b.SetListEntryTypes(nil)

	if err := b.Validate(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	h, err := b.Build(context.Background(), test.NewEnv(t))
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if err = h.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
}

// A test case for the list checker.
type listTestCase struct {
	addr   string
	result rpc.Code
	fail   bool
}

// Build a list handler with the given config.
func buildHandler(t *testing.T, cfg adapter.Config) (*handler, error) {
	info := GetInfo()
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(cfg)
	h, err := b.Build(context.Background(), test.NewEnv(t))
	if err != nil {
		return nil, err
	}
	return h.(*handler), nil
}

// Checks that the given test cases are executed as expected by the handler.
func checkCases(t *testing.T, cases []listTestCase, h *handler) {
	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			result, err := h.HandleListEntry(context.Background(), &listentry.Instance{Value: c.addr})
			if (err != nil) != c.fail {
				t.Errorf("Did not expect err '%v'", err)
			}

			if err != nil {
				if result.Status.Code != int32(c.result) {
					t.Errorf("Got '%v', expecting '%v'", result.Status.Code, c.result)
				}
			}
		})
	}
}

// Create a server that will serve the given data as the response on each request.
func setupServer(t *testing.T, data string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(data)); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
}

func containsListEntryTemplate(s []string) bool {
	for _, a := range s {
		if a == listentry.TemplateName {
			return true
		}
	}
	return false
}

func TestIPList(t *testing.T) {
	var listToServe interface{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := yaml.Marshal(listToServe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(out); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"11.11.11.11"},
		EntryType:       config.IP_ADDRESSES,
	}
	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	cases := []listTestCase{
		{"10.10.11.2", rpc.OK, false},
		{"9.9.9.1", rpc.OK, false},
		{"120.10.11.2", rpc.NOT_FOUND, false},
		{"11.11.11.11", rpc.OK, false},
		{"XYZ", rpc.INVALID_ARGUMENT, false},
	}

	listToServe = listPayload{
		WhiteList: []string{"10.10.11.2", "10.10.11.3", "9.9.9.9/28"},
	}
	h.fetchList()

	// exercise the NOP code
	h.fetchList()

	checkCases(t, cases, h)

	// now try to parse a list with errors
	entryCnt := h.list.numEntries()
	listToServe = listPayload{
		WhiteList: []string{"10.10.11.2", "X", "10.10.11.3"},
	}
	h.fetchList()
	if h.lastFetchError == nil {
		t.Errorf("Got success, expected error")
	}

	if h.list.numEntries() != entryCnt {
		t.Errorf("Got %d entries, expected %d", h.list.numEntries(), entryCnt)
	}

	// now try to parse a list in the wrong format
	listToServe = []string{"JUNK"}
	h.fetchList()
	if h.lastFetchError == nil {
		t.Error("Got success, expected error")
	}
	if h.list.numEntries() != entryCnt {
		t.Errorf("Got %d entries, expected %d", h.list.numEntries(), entryCnt)
	}

	if err := h.Close(); err != nil {
		t.Errorf("Unable to close adapter: %v", err)
	}
}

func TestStringList(t *testing.T) {
	ts := setupServer(t, "ABC\nDEF")
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"OVERRIDE"},
		EntryType:       config.STRINGS,
	}

	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	cases := []listTestCase{
		{"ABC", rpc.OK, false},
		{"DEF", rpc.OK, false},
		{"GHI", rpc.NOT_FOUND, false},
		{"OVERRIDE", rpc.OK, false},

		{"abc", rpc.NOT_FOUND, false},
		{"override", rpc.NOT_FOUND, false},
	}

	checkCases(t, cases, h)
}

func TestBlackStringList(t *testing.T) {
	ts := setupServer(t, "ABC\nDEF")
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"OVERRIDE"},
		EntryType:       config.STRINGS,
		Blacklist:       true,
	}

	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	cases := []listTestCase{
		{"ABC", rpc.PERMISSION_DENIED, false},
		{"DEF", rpc.PERMISSION_DENIED, false},
		{"GHI", rpc.OK, false},
		{"OVERRIDE", rpc.PERMISSION_DENIED, false},

		{"abc", rpc.OK, false},
		{"override", rpc.OK, false},
	}

	checkCases(t, cases, h)
}

func TestCaseInsensitiveStringList(t *testing.T) {
	ts := setupServer(t, "AbC\nDeF")
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"Override"},
		EntryType:       config.CASE_INSENSITIVE_STRINGS,
	}
	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	cases := []listTestCase{
		{"ABC", rpc.OK, false},
		{"DEF", rpc.OK, false},
		{"GHI", rpc.NOT_FOUND, false},
		{"OVERRIDE", rpc.OK, false},

		{"abc", rpc.OK, false},
		{"override", rpc.OK, false},
	}

	checkCases(t, cases, h)
}

func TestNoUrlStringList(t *testing.T) {
	cfg := config.Params{
		Overrides: []string{"OVERRIDE"},
		EntryType: config.STRINGS,
	}
	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	cases := []listTestCase{
		{"ABC", rpc.NOT_FOUND, false},
		{"OVERRIDE", rpc.OK, false},
		{"abc", rpc.NOT_FOUND, false},
		{"override", rpc.NOT_FOUND, false},
	}

	checkCases(t, cases, h)
}

func TestRegexList(t *testing.T) {
	var listToServe interface{}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := yaml.Marshal(listToServe)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(out); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"a*b"},
		EntryType:       config.REGEX,
	}
	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	cases := []listTestCase{
		{"abc", rpc.OK, false},
		{"B", rpc.NOT_FOUND, false},
	}

	listToServe = listPayload{
		WhiteList: []string{"a+.*", "efg"},
	}
	h.fetchList()

	// exercise the NOP code
	h.fetchList()

	checkCases(t, cases, h)

	// now try to parse a list with errors
	entryCnt := h.list.numEntries()
	listToServe = listPayload{
		WhiteList: []string{"10.10.11.2", "(", "10.10.11.3"},
	}
	h.fetchList()
	if h.lastFetchError == nil {
		t.Errorf("Got success, expected error")
	}

	if h.list.numEntries() != entryCnt {
		t.Errorf("Got %d entries, expected %d", h.list.numEntries(), entryCnt)
	}

	// now try to parse a list in the wrong format
	listToServe = []string{"("}
	h.fetchList()
	if h.lastFetchError == nil {
		t.Error("Got success, expected error")
	}
	if h.list.numEntries() != entryCnt {
		t.Errorf("Got %d entries, expected %d", h.list.numEntries(), entryCnt)
	}

	if err := h.Close(); err != nil {
		t.Errorf("Unable to close adapter: %v", err)
	}
}

func TestBadRegexOverride(t *testing.T) {
	cfg := &config.Params{
		Overrides: []string{
			"(", // invalid regex
		},
		EntryType: config.REGEX,
	}
	info := GetInfo()
	b := info.NewBuilder().(*builder)
	b.SetAdapterConfig(cfg)

	if err := b.Validate(); err == nil {
		t.Error("Expected validation error")
	}
}

func TestBadUrl(t *testing.T) {
	cfg := config.Params{
		ProviderUrl:     "https://localhost:80",
		RefreshInterval: 1 * time.Second,
		Ttl:             2 * time.Second,
	}
	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	if _, err := h.HandleListEntry(context.Background(), &listentry.Instance{Value: "JUNK"}); err == nil {
		t.Error("Got success, expected failure")
	}

	if err := h.Close(); err != nil {
		t.Errorf("Unable to close handler: %v", err)
	}
}

func TestIOErrors(t *testing.T) {
	serveError := true

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveError {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 10000 * time.Second,
		Ttl:             20000 * time.Second,
	}
	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	if _, err := h.HandleListEntry(context.Background(), &listentry.Instance{Value: "JUNK"}); err == nil {
		t.Error("Got success, expected failure")
	}

	// now try to process an uncooperative body
	serveError = false
	h.lastFetchError = nil
	h.readAll = func(r io.Reader) ([]byte, error) { return nil, errors.New("nothing good ever happens to me") }
	h.fetchList()

	if h.lastFetchError == nil {
		t.Errorf("Got success, expected failure")
	}

	if err := h.Close(); err != nil {
		t.Errorf("Unable to close handler: %v", err)
	}
}

func TestRegexErrors(t *testing.T) {
	ts := setupServer(t, "ABC\n[(\n])")
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		EntryType:       config.REGEX,
	}
	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	if h.lastFetchError == nil {
		t.Errorf("Got success, expected failure")
	}

	if err := h.Close(); err != nil {
		t.Errorf("Unable to close handler: %v", err)
	}
}

func TestRefreshAndPurge(t *testing.T) {
	var failServe int32

	// web server that returns valid data, until failServe is incremented at which point it
	// returns errors
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&failServe) > 0 {
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}

		if _, err := w.Write([]byte("ABC")); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()

	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Millisecond,
		Ttl:             2 * time.Millisecond,
		EntryType:       config.STRINGS,
	}

	h, err := buildHandler(t, &cfg)
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	// wait for the list to have been populated
	for {
		time.Sleep(1 * time.Millisecond)
		result, _ := h.hasData()
		if result {
			// list has been populated
			break
		}
	}

	// make sure everything is working OK
	_, err = h.HandleListEntry(context.Background(), &listentry.Instance{Value: "ABC"})
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	// cause the http server to start failing requests
	atomic.AddInt32(&failServe, 1)

	// wait for the list to have been purged
	for {
		time.Sleep(1 * time.Millisecond)
		result, err := h.hasData()
		if !result && err != nil {
			// list has been purged and failed to reload
			break
		}
	}

	// ensure we get an error now that the list has been purged
	_, err = h.HandleListEntry(context.Background(), &listentry.Instance{Value: "ABC"})
	if err == nil {
		t.Error("Got success, expected error")
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		cfg   config.Params
		field string
	}{
		{
			cfg:   config.Params{ProviderUrl: "Foo", RefreshInterval: 1 * time.Second, Ttl: 2 * time.Second},
			field: "providerUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: ":", RefreshInterval: 1 * time.Second, Ttl: 2 * time.Second},
			field: "providerUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http:", RefreshInterval: 1 * time.Second, Ttl: 2 * time.Second},
			field: "providerUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://", RefreshInterval: 1 * time.Second, Ttl: 2 * time.Second},
			field: "providerUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http:///FOO", RefreshInterval: 1 * time.Second, Ttl: 2 * time.Second},
			field: "providerUrl",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://foo.com", RefreshInterval: 0 * time.Second, Ttl: 2 * time.Second},
			field: "refreshInterval",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://foo.com", RefreshInterval: -1 * time.Second, Ttl: 2 * time.Second},
			field: "refreshInterval",
		},

		{
			cfg:   config.Params{ProviderUrl: "http://foo.com", RefreshInterval: 1 * time.Second, Ttl: 0 * time.Second},
			field: "ttl",
		},

		{
			cfg:   config.Params{CachingInterval: -1 * time.Second},
			field: "cachingInterval",
		},

		{
			cfg:   config.Params{CachingUseCount: -1},
			field: "cachingUseCount",
		},

		{
			cfg:   config.Params{EntryType: config.IP_ADDRESSES, Overrides: []string{"XYZ"}},
			field: "overrides",
		},

		{
			cfg:   config.Params{EntryType: config.IP_ADDRESSES, Overrides: []string{"1.2.3.4"}},
			field: "",
		},

		{
			cfg:   config.Params{EntryType: config.REGEX, Overrides: []string{"(["}},
			field: "overrides",
		},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			info := GetInfo()
			b := info.NewBuilder().(*builder)
			b.SetAdapterConfig(&c.cfg)

			err := b.Validate()
			if err == nil {
				if c.field != "" {
					t.Errorf("Got success, expecting error for field %s", c.field)
				}
			} else {
				if c.field == "" {
					t.Errorf("Got error %s, expected success", err)
				}
			}
		})
	}
}
