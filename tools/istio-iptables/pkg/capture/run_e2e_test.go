//go:build linux
// +build linux

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
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	// Create a new mount namespace.
	"github.com/howardjohn/unshare-go/mountns"
	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	// Create a new user namespace. This will map the current UID/GID to 0.
	"github.com/howardjohn/unshare-go/userns"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/util/assert"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

var initialized = &sync.Once{}

func setup(t *testing.T) {
	initialized.Do(func() {
		// Setup group namespace so iptables --gid-owner will work
		assert.NoError(t, userns.WriteGroupMap(map[uint32]uint32{userns.OriginalGID(): 0}))
		// Istio iptables expects to find a non-localhost IP in some interface
		assert.NoError(t, exec.Command("ip", "addr", "add", "240.240.240.240/32", "dev", "lo").Run())
		// Put a new file we have permission to access over xtables.lock
		xtables := filepath.Join(t.TempDir(), "xtables.lock")
		_, err := os.Create(xtables)
		assert.NoError(t, err)
		_ = os.Mkdir("/run", 0o777)
		_ = mountns.BindMount(xtables, "/run/xtables.lock")
	})
}

func TestIdempotentEquivalentRerun(t *testing.T) {
	setup(t)
	commonCases := GetCommonTestCases()
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
	scope := log.FindScope(log.DefaultScopeName)
	for _, tt := range commonCases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConstructTestConfig()
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
				iptConfigurator := NewIptablesConfigurator(cfg, ext)
				assert.NoError(t, iptConfigurator.Run())
				residueExists, deltaExists := VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptVer, &ipt6Ver)
				assert.Equal(t, residueExists, false)
				assert.Equal(t, deltaExists, true)
			}()

			// First Pass
			cfg.Reconcile = false
			iptConfigurator := NewIptablesConfigurator(cfg, ext)
			assert.NoError(t, iptConfigurator.Run())
			residueExists, deltaExists := VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			// Second Pass
			iptConfigurator = NewIptablesConfigurator(cfg, ext)
			assert.NoError(t, iptConfigurator.Run())

			// Execution should fail if force-apply is used and chains exists
			cfg.ForceApply = true
			iptConfigurator = NewIptablesConfigurator(cfg, ext)
			assert.Error(t, iptConfigurator.Run())
			cfg.ForceApply = false
		})
	}
}

func TestIdempotentUnequaledRerun(t *testing.T) {
	setup(t)
	commonCases := GetCommonTestCases()
	ext := &dep.RealDependencies{
		UsePodScopedXtablesLock: false,
		NetworkNamespace:        "",
	}
	iptVer, err := ext.DetectIptablesVersion(false)
	if err != nil {
		t.Fatalf("Can't detect iptables version")
	}

	ipt6Ver, err := ext.DetectIptablesVersion(true)
	if err != nil {
		t.Fatalf("Can't detect ip6tables version")
	}
	scope := log.FindScope(log.DefaultScopeName)
	for _, tt := range commonCases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConstructTestConfig()
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

			defer func() {
				// Final Cleanup
				cfg.CleanupOnly = true
				cfg.Reconcile = false
				iptConfigurator := NewIptablesConfigurator(cfg, ext)
				assert.NoError(t, iptConfigurator.Run())
				residueExists, deltaExists := VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptVer, &ipt6Ver)
				assert.Equal(t, residueExists, true) // residue found due to extra OUTPUT rule
				assert.Equal(t, deltaExists, true)
				// Remove additional rule
				cmd := exec.Command("iptables", "-t", "nat", "-D", "OUTPUT", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
				}
				residueExists, deltaExists = VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptVer, &ipt6Ver)
				assert.Equal(t, residueExists, false, "found unexpected residue on final pass")
				assert.Equal(t, deltaExists, true, "found no delta on final pass")
			}()

			// First Pass
			iptConfigurator := NewIptablesConfigurator(cfg, ext)
			assert.NoError(t, iptConfigurator.Run())
			residueExists, deltaExists := VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true, "did not find residue on first pass")
			assert.Equal(t, deltaExists, false, "found delta on first pass")

			// Diverge from installation
			cmd := exec.Command("iptables", "-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			// Apply not required after tainting non-ISTIO chains with extra rules
			residueExists, deltaExists = VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true, "did not find residue on second pass")
			assert.Equal(t, deltaExists, false, "found delta on second pass")

			cmd = exec.Command("iptables", "-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			// Apply required after tainting ISTIO chains
			residueExists, deltaExists = VerifyIptablesState(scope, iptConfigurator.ext, iptConfigurator.ruleBuilder, &iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true, "did not find residue on third pass")
			assert.Equal(t, deltaExists, true, "found no delta on third pass")

			// Fail is expected if cleanup is skipped
			cfg.Reconcile = false
			iptConfigurator = NewIptablesConfigurator(cfg, ext)
			assert.Error(t, iptConfigurator.Run())

			// Second pass with cleanup
			cfg.Reconcile = true
			iptConfigurator = NewIptablesConfigurator(cfg, ext)
			assert.NoError(t, iptConfigurator.Run())
		})
	}
}
