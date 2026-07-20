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
	"bytes"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	"github.com/howardjohn/unshare-go/userns"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/pkg/test/util/assert"
	iptablescapture "istio.io/istio/tools/istio-iptables/pkg/capture"
	iptablesconstants "istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func createHostsideProbeIpset(isV6 bool) (ipset.IPSet, error) {
	linDeps := ipset.RealNlDeps()
	probeSet, err := ipset.NewIPSet(config.ProbeIPSet, isV6, linDeps)
	if err != nil {
		return probeSet, err
	}
	probeSet.Flush()
	return probeSet, nil
}

func TestIdempotentEquivalentInPodRerun(t *testing.T) {
	setup(t)

	tests := GetCommonInPodTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)

			deps := &dep.RealDependencies{}
			_, iptConfiguratorPod, err := NewIptablesConfigurator(cfg, cfg, deps, deps, EmptyNlDeps())
			builder := iptConfiguratorPod.AppendInpodRules(tt.podOverrides)
			if err != nil {
				t.Fatalf("failed to setup iptables configurator: %v", err)
			}
			defer func() {
				assert.NoError(t, iptConfiguratorPod.DeleteInpodRules(log))
				residueExists, deltaExists := iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
				assert.Equal(t, residueExists, false)
				assert.Equal(t, deltaExists, true)
			}()
			assert.NoError(t, iptConfiguratorPod.CreateInpodRules(scopes.CNIAgent, tt.podOverrides))
			residueExists, deltaExists := iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			t.Log("Starting cleanup")
			// Cleanup, should work
			assert.NoError(t, iptConfiguratorPod.DeleteInpodRules(log))
			residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
			assert.Equal(t, residueExists, false)
			assert.Equal(t, deltaExists, true)

			t.Log("Second run")
			// Apply should work again
			assert.NoError(t, iptConfiguratorPod.CreateInpodRules(scopes.CNIAgent, tt.podOverrides))
			residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			t.Log("Third run")
			assert.NoError(t, iptConfiguratorPod.CreateInpodRules(scopes.CNIAgent, tt.podOverrides))
		})
	}
}

func TestIdempotentUnequalInPodRerun(t *testing.T) {
	setup(t)

	tests := GetCommonInPodTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)
			var stdout, stderr bytes.Buffer
			deps := &dep.RealDependencies{}
			_, iptConfiguratorPod, err := NewIptablesConfigurator(cfg, cfg, deps, deps, EmptyNlDeps())
			builder := iptConfiguratorPod.AppendInpodRules(tt.podOverrides)
			if err != nil {
				t.Fatalf("failed to setup iptables configurator: %v", err)
			}

			defer func() {
				assert.NoError(t, iptConfiguratorPod.DeleteInpodRules(log))
				residueExists, deltaExists := iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
				assert.Equal(t, residueExists, true)
				assert.Equal(t, deltaExists, true)
				// Remove additional rule
				cmd := exec.Command(iptConfiguratorPod.iptV.DetectedBinary, "-t", "nat", "-D", "OUTPUT", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
				}
				residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
				assert.Equal(t, residueExists, false)
				assert.Equal(t, deltaExists, true)
			}()

			assert.NoError(t, iptConfiguratorPod.CreateInpodRules(scopes.CNIAgent, tt.podOverrides))
			residueExists, deltaExists := iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			// Diverge by creating new ISTIO chains
			cmd := exec.Command(iptConfiguratorPod.iptV.DetectedBinary, "-t", "nat", "-N", "ISTIO_TEST")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			cmd = exec.Command(iptConfiguratorPod.iptV.DetectedBinary, "-t", "nat", "-A", "OUTPUT", "-j", "ISTIO_TEST")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			cmd = exec.Command(iptConfiguratorPod.iptV.DetectedBinary, "-t", "nat", "-A", "ISTIO_TEST", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			// Apply required after tempering with ISTIO chains
			residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, true)

			// Creating new inpod rules should fail if reconciliation is disabled
			cfg.Reconcile = false
			assert.Error(t, iptConfiguratorPod.CreateInpodRules(scopes.CNIAgent, tt.podOverrides))

			// Creating new inpod rules should succeed if reconciliation is enabled
			cfg.Reconcile = true
			assert.NoError(t, iptConfiguratorPod.CreateInpodRules(scopes.CNIAgent, tt.podOverrides))
			residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			// Jump added by tempering shall no longer exist
			cmd = exec.Command(iptConfiguratorPod.iptV.DetectedBinary, "-t", "nat", "-C", "OUTPUT", "-j", "ISTIO_TEST")
			assert.Error(t, cmd.Run())

			// Diverge from installation
			cmd = exec.Command(iptConfiguratorPod.iptV.DetectedBinary, "-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			// No delta after tempering with non-ISTIO chains
			residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfiguratorPod.ext, builder, &iptConfiguratorPod.iptV, &iptConfiguratorPod.ipt6V)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)
		})
	}
}

func TestIptablesHostCleanRoundTrip(t *testing.T) {
	setup(t)

	tests := GetCommonHostTestCases()

	ext := &dep.RealDependencies{
		UsePodScopedXtablesLock: false,
		NetworkNamespace:        "",
	}
	iptVer, err := ext.DetectIptablesVersion(false)
	if err != nil {
		t.Fatalf("Can't detect iptables version: %v", err)
	}
	ipt6Ver, err := ext.DetectIptablesVersion(true)
	if err != nil {
		t.Fatalf("Can't detect ip6tables version")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)

			deps := &dep.RealDependencies{}
			set, err := createHostsideProbeIpset(true)
			if err != nil {
				t.Fatalf("failed to create hostside probe ipset: %v", err)
			}
			defer func() {
				assert.NoError(t, set.DestroySet())
			}()

			iptConfigurator, _, err := NewIptablesConfigurator(cfg, cfg, deps, deps, RealNlDeps())
			builder := iptConfigurator.AppendHostRules()
			if err != nil {
				t.Fatalf("failed to setup iptables configurator: %v", err)
			}
			defer func() {
				iptConfigurator.DeleteHostRules()
				residueExists, deltaExists := iptablescapture.VerifyIptablesState(log, iptConfigurator.ext, builder, &iptVer, &ipt6Ver)
				assert.Equal(t, residueExists, false)
				assert.Equal(t, deltaExists, true)
			}()

			assert.NoError(t, iptConfigurator.CreateHostRulesForHealthChecks())
			residueExists, deltaExists := iptablescapture.VerifyIptablesState(log, iptConfigurator.ext, builder, &iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			// Round-trip deletion and recreation to test clean-up and re-setup
			t.Log("Starting cleanup")
			iptConfigurator.DeleteHostRules()
			residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfigurator.ext, builder, &iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, false)
			assert.Equal(t, deltaExists, true)

			t.Log("Second run")
			assert.NoError(t, iptConfigurator.CreateHostRulesForHealthChecks())
			residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfigurator.ext, builder, &iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			t.Log("Third run")
			assert.NoError(t, iptConfigurator.CreateHostRulesForHealthChecks())
		})
	}
}

// TestEnsureHostRulesRepairsExternalFlushE2E reproduces istio/istio#60607 against a
// real kernel: after an external actor flushes the ISTIO_POSTRT chain, EnsureHostRules
// must detect the drift and repair the host rules back to a converged state.
func TestEnsureHostRulesRepairsExternalFlushE2E(t *testing.T) {
	setup(t)

	ext := &dep.RealDependencies{
		UsePodScopedXtablesLock: false,
		NetworkNamespace:        "",
	}
	iptVer, err := ext.DetectIptablesVersion(false)
	if err != nil {
		t.Fatalf("Can't detect iptables version: %v", err)
	}
	ipt6Ver, err := ext.DetectIptablesVersion(true)
	if err != nil {
		t.Fatalf("Can't detect ip6tables version")
	}

	cfg := constructTestConfig()

	deps := &dep.RealDependencies{}
	set, err := createHostsideProbeIpset(true)
	if err != nil {
		t.Fatalf("failed to create hostside probe ipset: %v", err)
	}
	defer func() {
		assert.NoError(t, set.DestroySet())
	}()

	iptConfigurator, _, err := NewIptablesConfigurator(cfg, cfg, deps, deps, RealNlDeps())
	if err != nil {
		t.Fatalf("failed to setup iptables configurator: %v", err)
	}
	builder := iptConfigurator.AppendHostRules()
	defer func() {
		iptConfigurator.DeleteHostRules()
	}()

	assert.NoError(t, iptConfigurator.CreateHostRulesForHealthChecks())

	// When converged, EnsureHostRules must be a read-only no-op
	repaired, err := iptConfigurator.EnsureHostRules()
	assert.NoError(t, err)
	assert.Equal(t, repaired, false)

	// Simulate an external flush (the issue's reproduction step): empty the
	// ISTIO_POSTRT chain but keep the chain and the jump
	if _, err := ext.Run(log, false, iptablesconstants.IPTables, &iptVer, nil,
		"-t", "nat", "-F", ChainHostPostrouting); err != nil {
		t.Fatalf("failed to simulate external flush: %v", err)
	}
	_, deltaExists := iptablescapture.VerifyIptablesState(log, iptConfigurator.ext, builder, &iptVer, &ipt6Ver)
	assert.Equal(t, deltaExists, true)

	// EnsureHostRules must detect the drift and repair it
	repaired, err = iptConfigurator.EnsureHostRules()
	assert.NoError(t, err)
	assert.Equal(t, repaired, true)

	residueExists, deltaExists := iptablescapture.VerifyIptablesState(log, iptConfigurator.ext, builder, &iptVer, &ipt6Ver)
	assert.Equal(t, residueExists, true)
	assert.Equal(t, deltaExists, false)

	// A more thorough breakage: remove the chain and the jump altogether,
	// which must also be repaired
	iptConfigurator.DeleteHostRules()
	repaired, err = iptConfigurator.EnsureHostRules()
	assert.NoError(t, err)
	assert.Equal(t, repaired, true)

	residueExists, deltaExists = iptablescapture.VerifyIptablesState(log, iptConfigurator.ext, builder, &iptVer, &ipt6Ver)
	assert.Equal(t, residueExists, true)
	assert.Equal(t, deltaExists, false)
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
