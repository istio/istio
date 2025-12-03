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

package nodeagent

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	// Create a new user namespace. This will map the current UID/GID to 0.
	"github.com/howardjohn/unshare-go/userns"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/pkg/test/util/assert"
)

// TestDetectIptablesArtifacts is an e2e test that validates the iptable artifacts on the network.
// It runs in an isolated network namespace created by unshare-go, which provides a clean environment
// with root-like capabilities for netlink operations. It validates the upgrade path from iptables to
// nftables backend by simulating various scenarios.
func TestDetectIptablesArtifacts(t *testing.T) {
	setup(t)

	tests := []struct {
		name             string
		enableIPv6       bool
		setupFunc        func(t *testing.T)
		cleanupFunc      func(t *testing.T)
		expectedDetected bool
		description      string
	}{
		{
			name:       "v4_ipset_exists",
			enableIPv6: false,
			setupFunc: func(t *testing.T) {
				v4Name := fmt.Sprintf(ipset.V4Name, config.ProbeIPSet)
				err := netlink.IpsetCreate(v4Name, "hash:ip", netlink.IpsetCreateOptions{Replace: true})
				if err != nil && !strings.Contains(err.Error(), "exist") {
					t.Fatalf("failed to create v4 ipset: %v", err)
				}
				t.Logf("Created v4 IPset: %s", v4Name)
			},
			cleanupFunc: func(t *testing.T) {
				v4Name := fmt.Sprintf(ipset.V4Name, config.ProbeIPSet)
				_ = netlink.IpsetDestroy(v4Name)
			},
			expectedDetected: true,
			description:      "Should detect when v4 IPset exists (simulates an existing iptables deployment)",
		},
		{
			name:       "v6_ipset_exists",
			enableIPv6: true,
			setupFunc: func(t *testing.T) {
				v6Name := fmt.Sprintf(ipset.V6Name, config.ProbeIPSet)
				err := netlink.IpsetCreate(v6Name, "hash:ip", netlink.IpsetCreateOptions{
					Family:  unix.AF_INET6,
					Replace: true,
				})
				if err != nil && !strings.Contains(err.Error(), "exist") {
					t.Fatalf("failed to create v6 ipset: %v", err)
				}
				t.Logf("Created v6 IPset: %s", v6Name)
			},
			cleanupFunc: func(t *testing.T) {
				v6Name := fmt.Sprintf(ipset.V6Name, config.ProbeIPSet)
				_ = netlink.IpsetDestroy(v6Name)
			},
			expectedDetected: true,
			description:      "Should detect when v6 IPset exists",
		},
		{
			name:       "both_v4_and_v6_ipsets_exist",
			enableIPv6: true,
			setupFunc: func(t *testing.T) {
				v4Name := fmt.Sprintf(ipset.V4Name, config.ProbeIPSet)
				v6Name := fmt.Sprintf(ipset.V6Name, config.ProbeIPSet)

				err := netlink.IpsetCreate(v4Name, "hash:ip", netlink.IpsetCreateOptions{Replace: true})
				if err != nil && !strings.Contains(err.Error(), "exist") {
					t.Fatalf("failed to create v4 ipset: %v", err)
				}

				err = netlink.IpsetCreate(v6Name, "hash:ip", netlink.IpsetCreateOptions{
					Family:  unix.AF_INET6,
					Replace: true,
				})
				if err != nil && !strings.Contains(err.Error(), "exist") {
					t.Fatalf("failed to create v6 ipset: %v", err)
				}
				t.Logf("Created both v4 and v6 IPsets")
			},
			cleanupFunc: func(t *testing.T) {
				v4Name := fmt.Sprintf(ipset.V4Name, config.ProbeIPSet)
				v6Name := fmt.Sprintf(ipset.V6Name, config.ProbeIPSet)
				_ = netlink.IpsetDestroy(v4Name)
				_ = netlink.IpsetDestroy(v6Name)
			},
			expectedDetected: true,
			description:      "Should detect when both v4 and v6 IPsets exist",
		},
		{
			name:             "no_ipsets_exist",
			enableIPv6:       true,
			setupFunc:        func(t *testing.T) {},
			cleanupFunc:      func(t *testing.T) {},
			expectedDetected: false,
			description:      "Should not detect artifacts in clean state (simulates a fresh setup)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.description)

			tt.setupFunc(t)
			defer tt.cleanupFunc(t)

			detected, _ := detectIptablesArtifacts(tt.enableIPv6)
			assert.Equal(t, detected, tt.expectedDetected)
		})
	}
}

var initialized = &sync.Once{}

// setup initializes the test environment using unshare-go.
// Importing "github.com/howardjohn/unshare-go/netns" causes the test to run in an isolated network
// namespace and userns provides user namespace mapping.
func setup(t *testing.T) {
	initialized.Do(func() {
		// Map current GID to root (0) in the user namespace
		// This gives us the necessary privileges for netlink operations (CAP_NET_ADMIN)
		assert.NoError(t, userns.WriteGroupMap(map[uint32]uint32{userns.OriginalGID(): 0}))
		t.Log("Successfully initialized an isolated test network namespace with root capabilities")
	})
}
