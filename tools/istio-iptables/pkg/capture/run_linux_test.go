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

package capture

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	// Create a new user namespace. This will map the current UID/GID to 0.
	"github.com/howardjohn/unshare-go/userns"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/assert"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func TestIdempotentEquivalentRerun(t *testing.T) {
	setup(t)
	commonCases := getCommonTestCases()
	ext := &dep.RealDependencies{
		UsePodScopedXtablesLock: false,
		NetworkNamespace:        "",
	}
	scope := log.FindScope(log.DefaultScopeName)
	for _, tt := range commonCases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)
			// Override UID and GID otherwise test will fail in the linux namespace from unshare-go lib
			cfg.ProxyUID = "0"
			cfg.ProxyGID = "0"
			if cfg.OwnerGroupsExclude != "" {
				cfg.OwnerGroupsInclude = "0"
			}
			if cfg.OwnerGroupsInclude != "" {
				cfg.OwnerGroupsInclude = "0"
			}

			defer func() {
				// Final Cleanup
				cfg.CleanupOnly = true
				cfg.Reconcile = false
				iptConfigurator, err := NewIptablesConfigurator(cfg, ext)
				if err != nil {
					t.Fatal("can't detect iptables")
				}
				assert.NoError(t, iptConfigurator.Run())
				residueExists, deltaExists := VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptConfigurator.iptV, &iptConfigurator.ipt6V)
				assert.Equal(t, residueExists, false)
				assert.Equal(t, deltaExists, true)
			}()

			// First Pass
			cfg.Reconcile = false
			iptConfigurator, err := NewIptablesConfigurator(cfg, ext)
			if err != nil {
				t.Fatal("can't detect iptables")
			}
			assert.NoError(t, iptConfigurator.Run())
			residueExists, deltaExists := VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptConfigurator.iptV, &iptConfigurator.ipt6V)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			// Second Pass
			iptConfigurator, err = NewIptablesConfigurator(cfg, ext)
			if err != nil {
				t.Fatal("can't detect iptables")
			}
			assert.NoError(t, iptConfigurator.Run())

			// Execution should fail if force-apply is used and chains exists
			cfg.ForceApply = true
			iptConfigurator, err = NewIptablesConfigurator(cfg, ext)
			if err != nil {
				t.Fatal("can't detect iptables")
			}
			assert.Error(t, iptConfigurator.Run())
			cfg.ForceApply = false
		})
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

func TestIdempotentUnequaledRerun(t *testing.T) {
	setup(t)
	commonCases := getCommonTestCases()
	ext := &dep.RealDependencies{
		UsePodScopedXtablesLock: false,
		NetworkNamespace:        "",
	}
	scope := log.FindScope(log.DefaultScopeName)
	for _, tt := range commonCases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)
			// Override UID and GID otherwise test will fail in the linux namespace from unshare-go lib
			cfg.ProxyUID = "0"
			cfg.ProxyGID = "0"
			var stdout, stderr bytes.Buffer
			if cfg.OwnerGroupsExclude != "" {
				cfg.OwnerGroupsInclude = "0"
			}
			if cfg.OwnerGroupsInclude != "" {
				cfg.OwnerGroupsInclude = "0"
			}
			iptConfigurator, err := NewIptablesConfigurator(cfg, ext)

			defer func() {
				// Final Cleanup
				iptConfigurator.cfg.CleanupOnly = true
				iptConfigurator.cfg.Reconcile = false
				assert.NoError(t, err)
				assert.NoError(t, iptConfigurator.Run())
				residueExists, deltaExists := VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptConfigurator.iptV, &iptConfigurator.ipt6V)
				assert.Equal(t, residueExists, true) // residue found due to extra OUTPUT rule
				assert.Equal(t, deltaExists, true)
				// Remove additional rule
				cmd := exec.Command(iptConfigurator.iptV.DetectedBinary, "-t", "nat", "-D", "OUTPUT", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
				}
				residueExists, deltaExists = VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptConfigurator.iptV, &iptConfigurator.ipt6V)
				assert.Equal(t, residueExists, false, "found unexpected residue on final pass")
				assert.Equal(t, deltaExists, true, "found no delta on final pass")
			}()

			// First Pass
			assert.NoError(t, err)
			assert.NoError(t, iptConfigurator.Run())
			residueExists, deltaExists := VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptConfigurator.iptV, &iptConfigurator.ipt6V)
			assert.Equal(t, residueExists, true, "did not find residue on first pass")
			assert.Equal(t, deltaExists, false, "found delta on first pass")

			// Diverge from installation
			cmd := exec.Command(iptConfigurator.iptV.DetectedBinary, "-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			// Apply not required after tainting non-ISTIO chains with extra rules
			residueExists, deltaExists = VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptConfigurator.iptV, &iptConfigurator.ipt6V)
			assert.Equal(t, residueExists, true, "did not find residue on second pass")
			assert.Equal(t, deltaExists, false, "found delta on second pass")

			cmd = exec.Command(iptConfigurator.iptV.DetectedBinary, "-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			// Apply required after tainting ISTIO chains
			residueExists, deltaExists = VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptConfigurator.iptV, &iptConfigurator.ipt6V)
			assert.Equal(t, residueExists, true, "did not find residue on third pass")
			assert.Equal(t, deltaExists, true, "found no delta on third pass")

			// Fail is expected if cleanup is skipped
			iptConfigurator.cfg.Reconcile = false
			assert.NoError(t, err)
			assert.Error(t, iptConfigurator.Run())

			// Second pass with cleanup
			iptConfigurator.cfg.Reconcile = true
			assert.NoError(t, err)
			assert.NoError(t, iptConfigurator.Run())
		})
	}
}
