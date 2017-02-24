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
	"strings"
	"sync"
	"testing"
	"time"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/expr"
)

type mtest struct {
	gcContent string
	gc        string
	scContent string
	sc        string
	v         map[string]adapter.ConfigValidator
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
		{"", "", "", "", nil, "no such file or directory"},
		{sGlobalConfig, "globalconfig", "", "", nil, "no such file or directory"},
		{sGlobalConfig, "globalconfig", sSvcConfig, "serviceconfig", nil, "failed validation"},
		{sGlobalConfigValid, "globalconfig", sSvcConfig2, "serviceconfig", map[string]adapter.ConfigValidator{
			"denyChecker": &lc{},
			"metrics":     &lc{},
			"listchecker": &lc{},
		}, ""},
	}
	for idx, mt := range mlist {
		loopDelay := time.Millisecond * 50
		vf := newVfinder(mt.v)
		ma := &managerArgs{
			aspectFinder:  vf.FindValidator,
			builderFinder: vf.FindValidator,
			findKinds:     vf.AdapterToAspectMapperFunc,
			eval:          evaluator,
			loopDelay:     loopDelay,
		}
		if mt.gc != "" {
			tmpfile, _ := ioutil.TempFile("", mt.gc)
			ma.globalConfig = tmpfile.Name()
			defer func() { _ = os.Remove(ma.globalConfig) }()
			_, _ = tmpfile.Write([]byte(mt.gcContent))
			_ = tmpfile.Close()
		}

		if mt.sc != "" {
			tmpfile, _ := ioutil.TempFile("", mt.sc)
			ma.serviceConfig = tmpfile.Name()
			defer func() { _ = os.Remove(ma.serviceConfig) }()
			_, _ = tmpfile.Write([]byte(mt.scContent))
			_ = tmpfile.Close()
		}
		testConfigManager(t, newmanager(ma), mt, idx, loopDelay)
	}
}

func newmanager(args *managerArgs) *Manager {
	return NewManager(args.eval, args.aspectFinder, args.builderFinder, args.findKinds, args.globalConfig, args.serviceConfig, args.loopDelay)
}

type managerArgs struct {
	eval          expr.Evaluator
	aspectFinder  ValidatorFinderFunc
	builderFinder ValidatorFinderFunc
	findKinds     AdapterToAspectMapperFunc
	loopDelay     time.Duration
	globalConfig  string
	serviceConfig string
}

func testConfigManager(t *testing.T, mgr *Manager, mt mtest, idx int, loopDelay time.Duration) {
	fl := &fakelistener{}
	mgr.Register(fl)

	mgr.Start()
	defer mgr.Close()

	le := mgr.LastError()

	if mt.errStr != "" && le == nil {
		t.Errorf("[%d] Expected an error %s Got nothing", idx, mt.errStr)
		return
	}

	if mt.errStr == "" && le != nil {
		t.Errorf("[%d] Unexpected an error %s", idx, le)
		return
	}

	if mt.errStr == "" && fl.rt == nil {
		t.Errorf("[%d] Config listener was not notified", idx)
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
		t.Errorf("[%d] Unexpected error. Expected %s\nGot: %s\n", idx, mt.errStr, le)
		return
	}
}
