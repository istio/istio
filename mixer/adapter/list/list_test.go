// Copyright 2017 Istio Authors
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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/adapter/list/config"
	"istio.io/mixer/pkg/adapter/test"
	"istio.io/mixer/template/listentry"
)

func TestBasic(t *testing.T) {
	info := GetBuilderInfo()

	if !containsListEntryTemplate(info.SupportedTemplates) {
		t.Error("Didn't find all expected supported templates")
	}

	builder := info.CreateHandlerBuilder()
	cfg := info.DefaultConfig

	if err := info.ValidateConfig(cfg); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	listEntryBuilder := builder.(listentry.HandlerBuilder)
	if err := listEntryBuilder.ConfigureListEntryHandler(nil); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	handler, err := builder.Build(cfg, test.NewEnv(t))
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	if err = handler.Close(); err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}
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

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"11.11.11.11"},
		EntryType:       config.IP_ADDRESSES,
	}

	h, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	leh := h.(*handler)

	cases := []struct {
		addr   string
		result rpc.Code
		fail   bool
	}{
		{"10.10.11.2", rpc.OK, false},
		{"9.9.9.1", rpc.OK, false},
		{"120.10.11.2", rpc.NOT_FOUND, false},
		{"11.11.11.11", rpc.OK, false},
		{"XYZ", rpc.INVALID_ARGUMENT, false},
	}

	listToServe = listPayload{
		WhiteList: []string{"10.10.11.2", "10.10.11.3", "9.9.9.9/28"},
	}
	leh.fetchList()

	// exercise the NOP code
	leh.fetchList()

	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			result, err := leh.HandleListEntry(context.Background(), &listentry.Instance{Value: c.addr})
			if (err != nil) != c.fail {
				t.Errorf("Did not expect err '%v'", err)
			}

			if err == nil {
				if result.Status.Code != int32(c.result) {
					t.Errorf("Got '%v', expecting '%v'", result.Status.Code, c.result)
				}
			}
		})
	}

	// now try to parse a list with errors
	entryCnt := leh.list.numEntries()
	listToServe = listPayload{
		WhiteList: []string{"10.10.11.2", "X", "10.10.11.3"},
	}
	leh.fetchList()
	if leh.lastFetchError == nil {
		t.Errorf("Got success, expected error")
	}

	if leh.list.numEntries() != entryCnt {
		t.Errorf("Got %d entries, expected %d", leh.list.numEntries(), entryCnt)
	}

	// now try to parse a list in the wrong format
	listToServe = []string{"JUNK"}
	leh.fetchList()
	if leh.lastFetchError == nil {
		t.Error("Got success, expected error")
	}
	if leh.list.numEntries() != entryCnt {
		t.Errorf("Got %d entries, expected %d", leh.list.numEntries(), entryCnt)
	}

	if err := leh.Close(); err != nil {
		t.Errorf("Unable to close adapter: %v", err)
	}
}

func TestStringList(t *testing.T) {
	listToServe := "ABC\nDEF"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(listToServe)); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"OVERRIDE"},
		EntryType:       config.STRINGS,
	}

	h, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	leh := h.(*handler)

	cases := []struct {
		addr   string
		result rpc.Code
		fail   bool
	}{
		{"ABC", rpc.OK, false},
		{"DEF", rpc.OK, false},
		{"GHI", rpc.NOT_FOUND, false},
		{"OVERRIDE", rpc.OK, false},

		{"abc", rpc.NOT_FOUND, false},
		{"override", rpc.NOT_FOUND, false},
	}

	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			result, err := leh.HandleListEntry(context.Background(), &listentry.Instance{Value: c.addr})
			if (err != nil) != c.fail {
				t.Errorf("Did not expect err '%v'", err)
			}

			if err == nil {
				if result.Status.Code != int32(c.result) {
					t.Errorf("Got '%v', expecting '%v'", result.Status.Code, c.result)
				}
			}
		})
	}
}

func TestBlackStringList(t *testing.T) {
	listToServe := "ABC\nDEF"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(listToServe)); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"OVERRIDE"},
		EntryType:       config.STRINGS,
		Blacklist:       true,
	}

	h, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	leh := h.(*handler)

	cases := []struct {
		addr   string
		result rpc.Code
		fail   bool
	}{
		{"ABC", rpc.PERMISSION_DENIED, false},
		{"DEF", rpc.PERMISSION_DENIED, false},
		{"GHI", rpc.OK, false},
		{"OVERRIDE", rpc.PERMISSION_DENIED, false},

		{"abc", rpc.OK, false},
		{"override", rpc.OK, false},
	}

	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			result, err := leh.HandleListEntry(context.Background(), &listentry.Instance{Value: c.addr})
			if (err != nil) != c.fail {
				t.Errorf("Did not expect err '%v'", err)
			}

			if err == nil {
				if result.Status.Code != int32(c.result) {
					t.Errorf("Got '%v', expecting '%v'", result.Status.Code, c.result)
				}
			}
		})
	}
}

func TestCaseInsensitiveStringList(t *testing.T) {
	listToServe := "AbC\nDeF"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(listToServe)); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Second,
		Ttl:             10 * time.Second,
		Overrides:       []string{"Override"},
		EntryType:       config.CASE_INSENSITIVE_STRINGS,
	}

	h, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	leh := h.(*handler)

	cases := []struct {
		addr   string
		result rpc.Code
		fail   bool
	}{
		{"ABC", rpc.OK, false},
		{"DEF", rpc.OK, false},
		{"GHI", rpc.NOT_FOUND, false},
		{"OVERRIDE", rpc.OK, false},

		{"abc", rpc.OK, false},
		{"override", rpc.OK, false},
	}

	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			result, err := leh.HandleListEntry(context.Background(), &listentry.Instance{Value: c.addr})
			if (err != nil) != c.fail {
				t.Errorf("Did not expect err '%v'", err)
			}

			if err == nil {
				if result.Status.Code != int32(c.result) {
					t.Errorf("Got '%v', expecting '%v'", result.Status.Code, c.result)
				}
			}
		})
	}
}

func TestNoUrlStringList(t *testing.T) {
	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		Overrides: []string{"OVERRIDE"},
		EntryType: config.STRINGS,
	}

	h, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	leh := h.(*handler)

	cases := []struct {
		addr   string
		result rpc.Code
		fail   bool
	}{
		{"ABC", rpc.NOT_FOUND, false},
		{"OVERRIDE", rpc.OK, false},
		{"abc", rpc.NOT_FOUND, false},
		{"override", rpc.NOT_FOUND, false},
	}

	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			result, err := leh.HandleListEntry(context.Background(), &listentry.Instance{Value: c.addr})
			if (err != nil) != c.fail {
				t.Errorf("Did not expect err '%v'", err)
			}

			if err == nil {
				if result.Status.Code != int32(c.result) {
					t.Errorf("Got '%v', expecting '%v'", result.Status.Code, c.result)
				}
			}
		})
	}
}

func TestBadUrl(t *testing.T) {
	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		ProviderUrl:     "http://abadurl.com",
		RefreshInterval: 1 * time.Second,
		Ttl:             2 * time.Second,
	}

	handler, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	listEntryHandler := handler.(listentry.Handler)
	if _, err := listEntryHandler.HandleListEntry(context.Background(), &listentry.Instance{Value: "JUNK"}); err == nil {
		t.Error("Got success, expected failure")
	}

	if err := handler.Close(); err != nil {
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

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 10000 * time.Second,
		Ttl:             20000 * time.Second,
	}

	h, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	leh := h.(*handler)
	if _, err := leh.HandleListEntry(context.Background(), &listentry.Instance{Value: "JUNK"}); err == nil {
		t.Error("Got success, expected failure")
	}

	// now try to process an uncooperative body
	serveError = false
	leh.lastFetchError = nil
	leh.readAll = func(r io.Reader) ([]byte, error) { return nil, errors.New("nothing good ever happens to me") }
	leh.fetchList()

	if leh.lastFetchError == nil {
		t.Errorf("Got success, expected failure")
	}

	if err := leh.Close(); err != nil {
		t.Errorf("Unable to close handler: %v", err)
	}
}

func TestRefreshAndPurge(t *testing.T) {
	listToServe := "ABC"
	var leh *handler

	wg := sync.WaitGroup{}
	wg.Add(1)
	var failServe int32
	purgeDetected := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if atomic.LoadInt32(&failServe) > 0 {
			w.WriteHeader(http.StatusMovedPermanently)

			if leh.list == nil {
				// the list was purged
				if !purgeDetected {
					purgeDetected = true
					wg.Done()
				}
			}

			return
		}

		if _, err := w.Write([]byte(listToServe)); err != nil {
			t.Errorf("w.Write failed: %v", err)
		}
	}))
	defer ts.Close()

	info := GetBuilderInfo()
	builder := info.CreateHandlerBuilder()
	cfg := config.Params{
		ProviderUrl:     ts.URL,
		RefreshInterval: 1 * time.Millisecond,
		Ttl:             2 * time.Millisecond,
		EntryType:       config.STRINGS,
	}

	h, err := builder.Build(&cfg, test.NewEnv(t))
	if err != nil {
		t.Fatalf("Got error %v, expecting success", err)
	}

	leh = h.(*handler)

	_, err = leh.HandleListEntry(context.Background(), &listentry.Instance{Value: "ABC"})
	if err != nil {
		t.Errorf("Got error %v, expecting success", err)
	}

	// cause the http server to start failing requests
	atomic.AddInt32(&failServe, 1)

	// wait for the list to have been purged
	wg.Wait()

	_, err = leh.HandleListEntry(context.Background(), &listentry.Instance{Value: "ABC"})
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
	}

	info := GetBuilderInfo()

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			err := info.ValidateConfig(&c.cfg)
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
