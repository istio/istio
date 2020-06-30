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

func TestDiscoverWithTimeout(t *testing.T) {
	tests := []struct {
		desc         string
		timeout      time.Duration
		envSetup     func(t *testing.T)
		envTeardown  func(t *testing.T)
		platExpectFn func(map[string]string) bool
		ignore       bool
	}{
		{
			desc:    "unknown-plat",
			timeout: 1 * time.Second,
			envSetup: func(*testing.T) {
				gcpMetadataVar.Name = "UNITTEST_GCP_METADATA"
				err := os.Unsetenv("GCP_METADATA")
				if err != nil {
					t.Errorf("Unable to set up test env: %v", err)
				}
			},
			envTeardown: func(*testing.T) {
				// re-set back to GCP_METADATA
				gcpMetadataVar.Name = "GCP_METADATA"
			},
			platExpectFn: func(m map[string]string) bool {
				// unknown has no metadata
				return len(m) == 0
			},
			// it seems there are some issues with testing on GKE.
			// locally, tests pass, but discovery of host goes beyond
			// just environment variables.
			// testOnGCE in cloud.google.com/go/compute/metadata uses
			// more than just environment variables and goes as far
			// as caling a GCP-specific endpoint on the machine.
			// until we can isolate tests from the environment better,
			// this will have to be ignored.
			ignore: true,
		},
		// todo add test to verify aws - currently not possible
		// 	because verifier reads from /sys/hypervisor/uuid which
		// 	isn't writable in a test env.
		{
			desc:    "gcp",
			timeout: 1 * time.Second,
			envSetup: func(*testing.T) {
				// dont want to mess with GCP_METADATA on GKE
				gcpMetadataVar.Name = "UNITTEST_GCP_METADATA"
				err := os.Setenv("UNITTEST_GCP_METADATA", "FOO|BAR|BAZ|MAR")
				if err != nil {
					t.Fatalf("Unable to setup environment: %v", err)
				}
			},
			envTeardown: func(t *testing.T) {
				// remove env var value
				err := os.Unsetenv("UNITTEST_GCP_METADATA")
				if err != nil {
					t.Fatalf("Unable to tear down: %v", err)
				}
				// re-set gcpMetadataVar to point to GCP_METADATA
				gcpMetadataVar.Name = "GCP_METADATA"
			},
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
			if !tt.ignore {
				tt.envSetup(t)
				defer tt.envTeardown(t)
				if got := DiscoverWithTimeout(tt.timeout); !tt.platExpectFn(got.Metadata()) {
					t.Errorf("TestDiscoveryWithTimeout(%s) %s => %v metadata not expected", tt.timeout.String(), tt.desc, got.Metadata())
				}
			}
		})
	}
}
