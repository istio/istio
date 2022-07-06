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

package platform

import (
	"fmt"
	"testing"
	"time"
)

type dummyHelper struct {
	username string
	password string
	err      error
}

func (h *dummyHelper) Get(serverURL string) (string, string, error) {
	return h.username, h.password, h.err
}

type testStep struct {
	helper       *dummyHelper
	serverURL    string
	wantUsername string
	wantPassword string
	wantError    bool
}

func (ts testStep) test(t *testing.T, helper *cachedHelper) {
	t.Helper()
	helper.internalHelper = ts.helper
	gotUsername, gotPassword, gotErr := helper.Get(ts.serverURL)
	if ts.wantError && gotErr == nil {
		t.Errorf("failed to raise the expected error")
	}
	if !ts.wantError && gotErr != nil {
		t.Errorf("unexpected error: %v", gotErr)
	}
	if gotUsername != ts.wantUsername {
		t.Errorf("unexpected username (- want, + got):\n - %q\n + %q", ts.wantUsername, gotUsername)
	}
	if gotPassword != ts.wantPassword {
		t.Errorf("unexpected password (- want, + got):\n - %q\n + %q", ts.wantPassword, gotPassword)
	}
}

func TestCachedHelper(t *testing.T) {
	initialHelper := &dummyHelper{
		username: "username-1",
		password: "password-1",
		err:      nil,
	}

	secondHelper := &dummyHelper{
		username: "username-2",
		password: "password-2",
		err:      nil,
	}

	cases := []struct {
		desc          string
		initialStep   testStep
		interval      time.Duration
		secondStep    testStep
		wantCacheSize int
	}{
		{
			desc: "cache hit",
			initialStep: testStep{
				helper:       initialHelper,
				serverURL:    "server-1",
				wantUsername: "username-1",
				wantPassword: "password-1",
				wantError:    false,
			},
			interval: 0,
			secondStep: testStep{
				helper:       secondHelper,
				serverURL:    "server-1",
				wantUsername: "username-1",
				wantPassword: "password-1",
				wantError:    false,
			},
			wantCacheSize: 1,
		},
		{
			desc: "cache miss",
			initialStep: testStep{
				helper:       initialHelper,
				serverURL:    "server-1",
				wantUsername: "username-1",
				wantPassword: "password-1",
				wantError:    false,
			},
			interval: time.Hour,
			secondStep: testStep{
				helper:       secondHelper,
				serverURL:    "server-1",
				wantUsername: "username-2",
				wantPassword: "password-2",
				wantError:    false,
			},
			wantCacheSize: 1,
		},
		{
			desc: "different server",
			initialStep: testStep{
				helper:       initialHelper,
				serverURL:    "server-1",
				wantUsername: "username-1",
				wantPassword: "password-1",
				wantError:    false,
			},
			interval: 0,
			secondStep: testStep{
				helper:       secondHelper,
				serverURL:    "server-2",
				wantUsername: "username-2",
				wantPassword: "password-2",
				wantError:    false,
			},
			wantCacheSize: 2,
		},
		{
			desc: "raise error",
			initialStep: testStep{
				helper:       initialHelper,
				serverURL:    "server-1",
				wantUsername: "username-1",
				wantPassword: "password-1",
				wantError:    false,
			},
			interval: time.Hour,
			secondStep: testStep{
				helper: &dummyHelper{
					username: "",
					password: "",
					err:      fmt.Errorf("error for tests"),
				},
				serverURL:    "server-1",
				wantUsername: "",
				wantPassword: "",
				wantError:    true,
			},
			wantCacheSize: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			cc := wrapHelperWithCache(nil, time.Nanosecond).(*cachedHelper)
			now := time.Now()
			cc.getNow = func() time.Time { return now }
			tc.initialStep.test(t, cc)
			now = now.Add(tc.interval)
			tc.secondStep.test(t, cc)
			gotCacheSize := len(cc.cache)
			if gotCacheSize != tc.wantCacheSize {
				t.Errorf("unexpected cache size (- want, + got):\n - %d\n + %d", tc.wantCacheSize, gotCacheSize)
			}
		})
	}
}
