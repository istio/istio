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

package config

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"istio.io/mixer/pkg/adapter"
)

type mtest struct {
	gcContent string
	gc        string
	scContent string
	sc        string
	ada       map[string]adapter.ConfigValidator
	asp       map[string]AspectValidator
	errStr    string
}

type fakelistener struct {
	called int
	rt     Resolver
	sync.Mutex
}

func (f *fakelistener) ConfigChange(cfg Resolver) {
	f.Lock()
	f.rt = cfg
	f.called++
	f.Unlock()
}
func (f *fakelistener) Called() int {
	f.Lock()
	called := f.called
	f.Unlock()
	return called
}

func TestConfigManager(t *testing.T) {
	evaluator := newFakeExpr()
	mlist := []mtest{
		{"", "", "", "", nil, nil, "no such file or directory"},
		{sGlobalConfig, "globalconfig", "", "", nil, nil, "no such file or directory"},
		{sGlobalConfig, "globalconfig", sSvcConfig, "serviceconfig", nil, nil, "failed validation"},
		{sGlobalConfigValid, "globalconfig", sSvcConfig2, "serviceconfig", map[string]adapter.ConfigValidator{
			"denyChecker": &lc{},
			"metrics":     &lc{},
			"listchecker": &lc{},
		}, map[string]AspectValidator{
			"denyChecker": &ac{},
			"metrics":     &ac{},
			"listchecker": &ac{},
		}, ""},
	}
	for idx, mt := range mlist {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			loopDelay := time.Millisecond * 50
			vf := newVfinder(mt.ada, mt.asp)
			gc := ""
			if mt.gc != "" {
				tmpfile, _ := ioutil.TempFile("", mt.gc)
				gc = tmpfile.Name()
				defer func() { _ = os.Remove(gc) }()
				_, _ = tmpfile.Write([]byte(mt.gcContent))
				_ = tmpfile.Close()
			}
			sc := ""
			if mt.sc != "" {
				tmpfile, _ := ioutil.TempFile("", mt.sc)
				sc = tmpfile.Name()
				defer func() { _ = os.Remove(sc) }()
				_, _ = tmpfile.Write([]byte(mt.scContent))
				_ = tmpfile.Close()
			}
			ma := NewManager(evaluator, vf.FindAspectValidator, vf.FindAdapterValidator, vf.AdapterToAspectMapperFunc, gc, sc, loopDelay)
			testConfigManager(t, ma, mt, loopDelay)
		})
	}
}

func testConfigManager(t *testing.T, mgr *Manager, mt mtest, loopDelay time.Duration) {
	fl := &fakelistener{}
	mgr.Register(fl)

	mgr.Start()
	defer mgr.Close()

	le := mgr.LastError()

	if mt.errStr != "" && le == nil {
		t.Fatalf("Expected an error %s Got nothing", mt.errStr)
	}

	if mt.errStr == "" && le != nil {
		t.Fatalf("Unexpected an error %s", le)
	}

	if mt.errStr == "" && fl.rt == nil {
		t.Error("Config listener was not notified")
	}

	if mt.errStr == "" && le == nil {
		called := fl.Called()
		if le == nil && called != 1 {
			t.Errorf("called Got: %d, want: 1", called)
		}
		// give mgr time to go thru the start Loop() go routine
		// fetchAndNotify should be indirectly called multiple times.
		time.Sleep(loopDelay * 2)
		// check again. should not change, no new data is available
		called = fl.Called()
		if le == nil && called != 1 {
			t.Errorf("called Got: %d, want: 1", called)
		}
		return
	}

	if !strings.Contains(le.Error(), mt.errStr) {
		t.Fatalf("Unexpected error. Expected %s\nGot: %s\n", mt.errStr, le)
	}
}
