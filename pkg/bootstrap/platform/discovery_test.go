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
	"testing"
	"time"
)

type platMetaFn func(map[string]string) bool

func TestDiscoverWithTimeout(t *testing.T) {
	tests := []struct {
		desc       string
		timeout    time.Duration
		platKey    string
		platVal    string
		platExpectFn platMetaFn
	}{
		{
			desc:       "no-plat",
			timeout:    1 * time.Second,
			platKey:    "",
			platVal:    "",
			platExpectFn: func(m map[string]string) bool {
				// unknown has no metadata
				return len(m) == 0
			},
		},
		// todo add test to verify aws - currently not possible
		// 	because verifier reads from /sys/hypervisor/uuid which
		// 	isn't writable in a test env.
		{
			desc:       "gcp",
			timeout:    1 * time.Second,
			platKey:    "GCP_METADATA",
			platVal:    "FOO|BAR|BAZ|MAR",
			platExpectFn: func(m map[string]string) bool {
				// PROJECT_ID|PROJECT_NUMBER|CLUSTER_NAME|CLUSTER_ZONE -> FOO|BAR|BAZ|MAR
				if proj, ok := m[GCPProject]; !ok || proj != "FOO" {
					return false
				}
				if projNum, ok := m[GCPProjectNumber]; !ok || projNum != "BAR" {
					return false
				}
				if clustName, ok := m[GCPCluster]; !ok || clustName != "BAZ" {
					return false
				}
				if clustZone, ok := m[GCPLocation]; !ok || clustZone != "MAR" {
					return false
				}
				return true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := os.Setenv(tt.platKey, tt.platVal)
			defer func() {
				err = os.Unsetenv(tt.platKey)
				if tt.platKey != "" && err != nil {
					t.Errorf("unable to tear down: %v", err)
				}
			}()
			if err != nil && tt.platKey != "" {
				t.Errorf("unable to setup: %v", err)
			}
			if got := DiscoverWithTimeout(tt.timeout); !tt.platExpectFn(got.Metadata()) {
				t.Errorf("TestDiscoveryWithTimeout(%s) %s: %v metadata not expected", tt.timeout.String(), tt.desc, got.Metadata())
			}
		})
	}
}
