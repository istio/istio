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

package iptables

import (
	"net/netip"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	"github.com/howardjohn/unshare-go/userns"

	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/pkg/test/util/assert"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func TestIptablesCleanRoundTrip(t *testing.T) {
	setup(t)
	tt := struct {
		name   string
		config func(cfg *Config)
	}{
		"default",
		func(cfg *Config) {
			cfg.RedirectDNS = true
		},
	}

	probeSNATipv4 := netip.MustParseAddr("169.254.7.127")
	probeSNATipv6 := netip.MustParseAddr("e9ac:1e77:90ca:399f:4d6d:ece2:2f9b:3164")

	cfg := &Config{}
	tt.config(cfg)

	deps := &dep.RealDependencies{}
	iptConfigurator, _, _ := NewIptablesConfigurator(cfg, deps, deps, EmptyNlDeps())
	assert.NoError(t, iptConfigurator.CreateInpodRules(scopes.CNIAgent, probeSNATipv4, probeSNATipv6, false))

	t.Log("starting cleanup")
	// Cleanup, should work
	assert.NoError(t, iptConfigurator.DeleteInpodRules())
	validateIptablesClean(t)

	t.Log("second run")
	// Add again, should still work
	assert.NoError(t, iptConfigurator.CreateInpodRules(scopes.CNIAgent, probeSNATipv4, probeSNATipv6, false))
}

func validateIptablesClean(t *testing.T) {
	cur := iptablesSave(t)
	if strings.Contains(cur, "ISTIO") {
		t.Fatalf("Istio rules leftover: %v", cur)
	}
	if strings.Contains(cur, "-A") {
		t.Fatalf("Rules: %v", cur)
	}
}

var initialized = &sync.Once{}

func setup(t *testing.T) {
	initialized.Do(func() {
		// Setup group namespace so iptables --gid-owner will work
		assert.NoError(t, userns.WriteGroupMap(map[uint32]uint32{userns.OriginalGID(): 0}))
		// Istio iptables expects to find a non-localhost IP in some interface
		assert.NoError(t, exec.Command("ip", "addr", "add", "240.240.240.240/32", "dev", "lo").Run())
	})

	tempDir := t.TempDir()
	xtables := filepath.Join(tempDir, "xtables.lock")
	// Override lockfile directory so that we don't need to unshare the mount namespace
	t.Setenv("XTABLES_LOCKFILE", xtables)
}

func iptablesSave(t *testing.T) string {
	res, err := exec.Command("iptables-save").CombinedOutput()
	assert.NoError(t, err)
	return string(res)
}
