// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nftables

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sigs.k8s.io/knftables"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/iptables"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	"istio.io/istio/tools/istio-nftables/pkg/builder"
)

// resetCaptured clears the captured transactions so a test can assert whether a
// subsequent operation ran any transaction at all.
func (m *MockNftablesCapture) resetCaptured() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.transactions = nil
}

type hostReconcileEnv struct {
	mock *MockNftablesCapture
	host *NftablesConfigurator
	// api is what the provider stub hands out; the configurator captures the stub at
	// construction time, so tests swap this field (not nftProviderVar) to change the
	// backend behavior mid-test.
	api builder.NftablesAPI
	// expectedRules is the number of SNAT rules the host postrouting chain is
	// expected to hold (1 for IPv4 only, 2 when IPv6 is enabled).
	expectedRules int
}

// setupHostReconcileTest wires a capture mock scoped to the ambient NAT table (so that
// ListRules, used by the convergence check, can resolve the table like the scoped
// interface the production read path requests) and stubs the kubelet UID provider.
func setupHostReconcileTest(t *testing.T, ipv6 bool) *hostReconcileEnv {
	t.Helper()

	cfg := constructTestConfig()
	cfg.EnableIPv6 = ipv6
	ext := &dep.DependenciesStub{}

	mock := &MockNftablesCapture{
		MockNftables: builder.NewMockNftables(knftables.InetFamily, AmbientNatTable),
	}
	env := &hostReconcileEnv{mock: mock, api: mock}
	originalProvider := nftProviderVar
	nftProviderVar = func(_ knftables.Family, _ string) (builder.NftablesAPI, error) {
		return env.api, nil
	}
	originalKubeletProvider := kubeletUIDProvider
	kubeletUIDProvider = func(string) (string, error) {
		return "1000", nil
	}
	t.Cleanup(func() {
		nftProviderVar = originalProvider
		kubeletUIDProvider = originalKubeletProvider
	})

	host, _, err := NewNftablesConfigurator(cfg, cfg, ext, ext, iptables.EmptyNlDeps())
	if err != nil {
		t.Fatalf("NewNftablesConfigurator: %v", err)
	}

	env.host = host
	env.expectedRules = 1
	if ipv6 {
		env.expectedRules = 2
	}
	return env
}

func (env *hostReconcileEnv) postroutingRules(t *testing.T) []*knftables.Rule {
	t.Helper()
	rules, err := env.mock.ListRules(context.Background(), PostroutingChain)
	if err != nil {
		t.Fatalf("listing postrouting rules: %v", err)
	}
	return rules
}

// TestCreateHostRulesProgramsProbeSets verifies that the host rules transaction is
// self-contained: it (re-)creates the probe IP sets the SNAT rules reference, so that a
// repair converges even after an external actor removed the whole table (e.g. a
// "flush ruleset"), instead of failing forever on a dangling set reference.
func TestCreateHostRulesProgramsProbeSets(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		t.Run(ipstr(ipv6), func(t *testing.T) {
			env := setupHostReconcileTest(t, ipv6)

			if err := env.host.CreateHostRulesForHealthChecks(); err != nil {
				t.Fatalf("CreateHostRulesForHealthChecks: %v", err)
			}

			if got := len(env.postroutingRules(t)); got != env.expectedRules {
				t.Fatalf("expected %d postrouting rules programmed, got %d", env.expectedRules, got)
			}

			env.mock.RLock()
			defer env.mock.RUnlock()
			if env.mock.Table == nil {
				t.Fatal("ambient nat table not programmed")
			}
			if env.mock.Table.Sets[config.ProbeIPSet+"-v4"] == nil {
				t.Fatal("v4 probe IP set missing from the host rules transaction")
			}
			if ipv6 && env.mock.Table.Sets[config.ProbeIPSet+"-v6"] == nil {
				t.Fatal("v6 probe IP set missing from the host rules transaction")
			}
		})
	}
}

// TestEnsureHostRulesConvergedIsReadOnly verifies that when the host rules are intact,
// the periodic reconcile does not run any nftables transaction (and reports no repair).
func TestEnsureHostRulesConvergedIsReadOnly(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		t.Run(ipstr(ipv6), func(t *testing.T) {
			env := setupHostReconcileTest(t, ipv6)

			if err := env.host.CreateHostRulesForHealthChecks(); err != nil {
				t.Fatalf("CreateHostRulesForHealthChecks: %v", err)
			}
			env.mock.resetCaptured()

			repaired, err := env.host.EnsureHostRules()
			if err != nil {
				t.Fatalf("EnsureHostRules: %v", err)
			}
			if repaired {
				t.Fatal("expected no repair to be reported when rules are intact")
			}
			if txs := env.mock.GetCapturedRules(); len(txs) != 0 {
				t.Fatalf("expected no transactions in steady state, got %d:\n%s", len(txs), strings.Join(txs, "\n"))
			}
		})
	}
}

// TestEnsureHostRulesRepairsExternalWipe verifies that deleting the whole ambient NAT
// table (the observable effect of e.g. a firewalld reload running "flush ruleset") is
// detected as drift and fully repaired, including the probe IP set structure the SNAT
// rules reference.
func TestEnsureHostRulesRepairsExternalWipe(t *testing.T) {
	for _, ipv6 := range []bool{false, true} {
		t.Run(ipstr(ipv6), func(t *testing.T) {
			env := setupHostReconcileTest(t, ipv6)

			if err := env.host.CreateHostRulesForHealthChecks(); err != nil {
				t.Fatalf("CreateHostRulesForHealthChecks: %v", err)
			}

			// Simulate the external actor: bypass the capture wrapper and delete the table.
			tx := env.mock.NewTransaction()
			tx.Delete(&knftables.Table{Name: AmbientNatTable, Family: knftables.InetFamily})
			if err := env.mock.MockNftables.Run(context.Background(), tx); err != nil {
				t.Fatalf("simulating external table wipe: %v", err)
			}

			repaired, err := env.host.EnsureHostRules()
			if err != nil {
				t.Fatalf("EnsureHostRules: %v", err)
			}
			if !repaired {
				t.Fatal("expected drift to be detected and repaired after an external table wipe")
			}
			if got := len(env.postroutingRules(t)); got != env.expectedRules {
				t.Fatalf("expected %d postrouting rules after repair, got %d", env.expectedRules, got)
			}

			// A follow-up tick must be read-only again.
			env.mock.resetCaptured()
			repaired, err = env.host.EnsureHostRules()
			if err != nil || repaired {
				t.Fatalf("expected converged steady state after repair, got repaired=%v err=%v", repaired, err)
			}
			if txs := env.mock.GetCapturedRules(); len(txs) != 0 {
				t.Fatalf("expected no transactions after repair converged, got %d", len(txs))
			}
		})
	}
}

// TestEnsureHostRulesRepairsRuleCountDrift verifies that a rule count mismatch in the
// istio-owned postrouting chain (a rule removed, or a foreign rule injected) is treated
// as drift and the chain is re-programmed back to the expected state.
func TestEnsureHostRulesRepairsRuleCountDrift(t *testing.T) {
	env := setupHostReconcileTest(t, false)

	if err := env.host.CreateHostRulesForHealthChecks(); err != nil {
		t.Fatalf("CreateHostRulesForHealthChecks: %v", err)
	}

	// Simulate a foreign rule injected into the istio-owned chain.
	tx := env.mock.NewTransaction()
	tx.Add(&knftables.Rule{
		Table:  AmbientNatTable,
		Family: knftables.InetFamily,
		Chain:  PostroutingChain,
		Rule:   "meta l4proto tcp counter accept",
	})
	if err := env.mock.MockNftables.Run(context.Background(), tx); err != nil {
		t.Fatalf("simulating foreign rule injection: %v", err)
	}

	repaired, err := env.host.EnsureHostRules()
	if err != nil {
		t.Fatalf("EnsureHostRules: %v", err)
	}
	if !repaired {
		t.Fatal("expected rule count drift to be detected and repaired")
	}
	if got := len(env.postroutingRules(t)); got != env.expectedRules {
		t.Fatalf("expected %d postrouting rules after repair, got %d", env.expectedRules, got)
	}
}

// TestEnsureHostRulesRepairsKubeletUIDChange verifies that a kubelet UID change across
// a kubelet restart (the rules embed the UID captured at programming time) is detected
// as drift and the rules are re-programmed with the new UID.
func TestEnsureHostRulesRepairsKubeletUIDChange(t *testing.T) {
	env := setupHostReconcileTest(t, false)

	if err := env.host.CreateHostRulesForHealthChecks(); err != nil {
		t.Fatalf("CreateHostRulesForHealthChecks: %v", err)
	}

	kubeletUIDProvider = func(string) (string, error) {
		return "2000", nil
	}
	env.mock.resetCaptured()

	repaired, err := env.host.EnsureHostRules()
	if err != nil {
		t.Fatalf("EnsureHostRules: %v", err)
	}
	if !repaired {
		t.Fatal("expected kubelet UID change to be detected and repaired")
	}
	txs := env.mock.GetCapturedRules()
	if len(txs) == 0 || !strings.Contains(strings.Join(txs, "\n"), "skuid 2000") {
		t.Fatalf("expected re-programmed rules to embed the new kubelet UID, got:\n%s", strings.Join(txs, "\n"))
	}

	// With the new UID recorded, the next tick must converge without writes.
	env.mock.resetCaptured()
	repaired, err = env.host.EnsureHostRules()
	if err != nil || repaired {
		t.Fatalf("expected converged steady state with the new UID, got repaired=%v err=%v", repaired, err)
	}
	if txs := env.mock.GetCapturedRules(); len(txs) != 0 {
		t.Fatalf("expected no transactions after UID repair converged, got %d", len(txs))
	}
}

// failingListRules simulates a read-side failure that is not a "not found" condition
// (e.g. the nft binary failing), which must NOT be treated as drift.
type failingListRules struct {
	*MockNftablesCapture
	listErr error
}

func (f *failingListRules) ListRules(context.Context, string) ([]*knftables.Rule, error) {
	return nil, f.listErr
}

// TestEnsureHostRulesSkipsRepairOnReadFailure verifies that when the convergence check
// itself fails (for reasons other than the table/chain being absent), the tick is
// skipped and reported as an error instead of blindly re-programming: the ticker
// provides the retry.
func TestEnsureHostRulesSkipsRepairOnReadFailure(t *testing.T) {
	env := setupHostReconcileTest(t, false)

	if err := env.host.CreateHostRulesForHealthChecks(); err != nil {
		t.Fatalf("CreateHostRulesForHealthChecks: %v", err)
	}

	listErr := errors.New("nft: resource temporarily unavailable")
	env.api = &failingListRules{MockNftablesCapture: env.mock, listErr: listErr}
	env.mock.resetCaptured()

	repaired, err := env.host.EnsureHostRules()
	if err == nil {
		t.Fatal("expected the read failure to be propagated")
	}
	if !errors.Is(err, listErr) {
		t.Fatalf("expected the underlying list error, got: %v", err)
	}
	if repaired {
		t.Fatal("expected no repair to be reported on a read failure")
	}
	if txs := env.mock.GetCapturedRules(); len(txs) != 0 {
		t.Fatalf("expected no transactions on a read failure, got %d", len(txs))
	}
}

// TestEnsureHostRulesSkipsRepairOnKubeletUIDError verifies that failing to determine
// the current kubelet UID (e.g. kubelet restarting at that instant) skips the tick
// without touching the healthy rules.
func TestEnsureHostRulesSkipsRepairOnKubeletUIDError(t *testing.T) {
	env := setupHostReconcileTest(t, false)

	if err := env.host.CreateHostRulesForHealthChecks(); err != nil {
		t.Fatalf("CreateHostRulesForHealthChecks: %v", err)
	}

	uidErr := errors.New("kubelet process not found")
	kubeletUIDProvider = func(string) (string, error) {
		return "", uidErr
	}
	env.mock.resetCaptured()

	repaired, err := env.host.EnsureHostRules()
	if err == nil {
		t.Fatal("expected the kubelet UID error to be propagated")
	}
	if repaired {
		t.Fatal("expected no repair to be reported on a kubelet UID read failure")
	}
	if txs := env.mock.GetCapturedRules(); len(txs) != 0 {
		t.Fatalf("expected no transactions on a kubelet UID read failure, got %d", len(txs))
	}
}

// TestEnsureHostRulesSelfSeedsWithoutCreate verifies that the reconcile is self-healing
// even if it somehow runs before the startup programming succeeded: the first tick
// programs the rules (reported as a repair), subsequent ticks converge.
func TestEnsureHostRulesSelfSeedsWithoutCreate(t *testing.T) {
	env := setupHostReconcileTest(t, false)

	repaired, err := env.host.EnsureHostRules()
	if err != nil {
		t.Fatalf("EnsureHostRules: %v", err)
	}
	if !repaired {
		t.Fatal("expected the first tick without prior programming to repair")
	}
	if got := len(env.postroutingRules(t)); got != env.expectedRules {
		t.Fatalf("expected %d postrouting rules after self-seeding, got %d", env.expectedRules, got)
	}

	env.mock.resetCaptured()
	repaired, err = env.host.EnsureHostRules()
	if err != nil || repaired {
		t.Fatalf("expected converged steady state after self-seeding, got repaired=%v err=%v", repaired, err)
	}
}
