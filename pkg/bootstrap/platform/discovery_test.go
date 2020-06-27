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
	"os"
	"reflect"
	"testing"
	"time"
)

func TestDiscoverWithTimeout(t *testing.T) {
	tests := []struct {
		desc       string
		timeout    time.Duration
		platKey    string
		platVal    string
		platExpect Environment
	}{
		{
			desc:       "no-plat",
			timeout:    1 * time.Second,
			platKey:    "",
			platVal:    "",
			platExpect: &Unknown{},
		},
		// todo add test to verify aws - currently not possible
		// 	because verifier reads from /sys/hypervisor/uuid which
		// 	isn't writable in a test env.
		{
			desc:       "gcp",
			timeout:    1 * time.Second,
			platKey:    "GCP_METADATA",
			platVal:    "FOO|BAR|BAZ|MAR",
			platExpect: &GcpEnv{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := os.Setenv(tt.platKey, tt.platVal)
			if err != nil && tt.platKey != "" {
				t.Errorf("unable to setup: %v", err)
			}
			if got := DiscoverWithTimeout(tt.timeout); reflect.TypeOf(tt.platExpect) != reflect.TypeOf(got) {
				t.Errorf("%s: want %v got %v", tt.desc, tt.platExpect, got)
			}
		})
	}
}
