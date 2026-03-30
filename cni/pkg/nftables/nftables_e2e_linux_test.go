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

package nftables

import (
	"bytes"
	"os/exec"
	"sync"
	"testing"

	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	// Create a new user namespace. This will map the current UID/GID to 0.
	"github.com/howardjohn/unshare-go/userns"
	"sigs.k8s.io/knftables"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/pkg/test/util/assert"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	"istio.io/istio/tools/istio-nftables/pkg/builder"
)

func dumpRuleset(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("nft", "list", "ruleset")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to dump nftables ruleset: %v\nStderr: %s", err, stderr.String())
	}
	return stdout.String()
}

func createHostsideProbeNftSets(t *testing.T) func() {
	t.Helper()

	setManager, err := NewHostSetManager(config.ProbeIPSet, true)
	if err != nil {
		t.Fatalf("Failed to create host set manager: %v", err)
	}

	return func() {
		_ = setManager.DestroySet()
	}
}

func TestPodIdempotentEquivalentRerun(t *testing.T) {
	setup(t)
	commonCases := GetCommonInPodTestCases()
	for _, tt := range commonCases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)

			nftProviderVar = func(_ knftables.Family, _ string) (builder.NftablesAPI, error) {
				return builder.NewNftImpl("", "")
			}

			deps := &dep.RealDependencies{}
			_, nftConfiguratorPod, err := NewNftablesConfigurator(cfg, cfg, deps, deps, iptables.EmptyNlDeps())
			assert.NoError(t, err)
			_, err = nftConfiguratorPod.AppendInpodRules(tt.podOverrides)
			if err != nil {
				t.Fatalf("failed to configure nftables rules in the pod network namespace: %v", err)
			}

			defer func() {
				assert.NoError(t, nftConfiguratorPod.DeleteInpodRules(log))
			}()

			firstPassDump := dumpRuleset(t)
			if firstPassDump == "" {
				t.Fatalf("First pass configuration resulted in an empty nftables ruleset dump")
			}

			_, nftConfiguratorPod, err = NewNftablesConfigurator(cfg, cfg, deps, deps, iptables.EmptyNlDeps())
			assert.NoError(t, err)
			_, err = nftConfiguratorPod.AppendInpodRules(tt.podOverrides)
			if err != nil {
				t.Fatalf("failed to configure nftables rules in the pod network namespace: %v", err)
			}

			secondPassDump := dumpRuleset(t)
			if secondPassDump == "" {
				t.Fatalf("Second pass configuration resulted in an empty nftables ruleset dump")
			}

			// The ruleset should be identical, despite the rerun
			assert.Equal(t, firstPassDump, secondPassDump, "nftables ruleset should be identical after a second run")
		})
	}
}

func TestHostIdempotentEquivalentRerun(t *testing.T) {
	setup(t)

	t.Run("hostprobe", func(t *testing.T) {
		cfg := constructTestConfig()

		// Set up the provider function to interact with the real system's nftables
		originalNftProvider := nftProviderVar
		nftProviderVar = func(family knftables.Family, table string) (builder.NftablesAPI, error) {
			return builder.NewNftImpl(family, table)
		}

		// Lets mock the kubelet UID provider for e2e tests
		originalKubeletProvider := kubeletUIDProvider
		kubeletUIDProvider = func(procPath string) (string, error) {
			return "1000", nil
		}

		defer func() {
			nftProviderVar = originalNftProvider
			kubeletUIDProvider = originalKubeletProvider
		}()

		// Create the probe IP sets that host rules expect to exist
		cleanupSets := createHostsideProbeNftSets(t)
		defer cleanupSets()

		deps := &dep.RealDependencies{}
		nftConfiguratorHost, _, err := NewNftablesConfigurator(cfg, cfg, deps, deps, iptables.EmptyNlDeps())
		assert.NoError(t, err)
		err = nftConfiguratorHost.CreateHostRulesForHealthChecks()
		if err != nil {
			t.Fatalf("failed to configure host side nftables rules: %v", err)
		}

		defer func() {
			nftConfiguratorHost.DeleteHostRules()
		}()

		firstPassDump := dumpRuleset(t)
		if firstPassDump == "" {
			t.Fatalf("First pass configuration resulted in an empty nftables ruleset nftDump")
		}

		nftConfiguratorHost, _, err = NewNftablesConfigurator(cfg, cfg, deps, deps, iptables.EmptyNlDeps())
		assert.NoError(t, err)
		err = nftConfiguratorHost.CreateHostRulesForHealthChecks()
		if err != nil {
			t.Fatalf("failed to configure nftables rules in the host side: %v", err)
		}

		secondPassDump := dumpRuleset(t)
		if secondPassDump == "" {
			t.Fatalf("Second pass configuration resulted in an empty nftables ruleset nftDump")
		}

		// The ruleset should be identical, despite the rerun
		assert.Equal(t, firstPassDump, secondPassDump, "nftables ruleset should be identical after a second run")
	})
}

var initialized = &sync.Once{}

func setup(t *testing.T) {
	initialized.Do(func() {
		// Setup group namespace so nftables skgid conditions will work
		assert.NoError(t, userns.WriteGroupMap(map[uint32]uint32{userns.OriginalGID(): 0}))
		// Istio nftables expects to find a non-localhost IP in some interface
		assert.NoError(t, exec.Command("ip", "addr", "add", "240.240.240.240/32", "dev", "lo").Run())
	})
}
