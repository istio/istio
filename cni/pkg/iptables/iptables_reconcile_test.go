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

package iptables

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
	iptablesconstants "istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

// hostStateStub builds on DependenciesStub to simulate pre-existing iptables state on
// the host: iptables-save returns a canned snapshot (or fails with saveErr), iptables -C
// succeeds or fails according to checkFails, iptables-restore fails for the first
// restoreFailures invocations, and every other command is only recorded.
type hostStateStub struct {
	dep.DependenciesStub
	saveOutput string
	saveErr    error
	checkFails bool
	// restoreFailures makes the first N iptables-restore invocations fail, to
	// exercise the repair retry
	restoreFailures int
}

func (s *hostStateStub) Run(logger *istiolog.Scope, quietLogging bool, cmd iptablesconstants.IptablesCmd,
	iptVer *dep.IptablesVersion, stdin io.ReadSeeker, args ...string,
) (*bytes.Buffer, error) {
	// Record every executed command so that tests can assert on them
	_, _ = s.DependenciesStub.Run(logger, quietLogging, cmd, iptVer, stdin, args...)
	if cmd == iptablesconstants.IPTablesSave {
		if s.saveErr != nil {
			return nil, s.saveErr
		}
		return bytes.NewBufferString(s.saveOutput), nil
	}
	if cmd == iptablesconstants.IPTablesRestore && s.restoreFailures > 0 {
		s.restoreFailures--
		return nil, errors.New("restore transiently failed")
	}
	if cmd == iptablesconstants.IPTables && slices.Contains(args, "-C") {
		if s.checkFails {
			return nil, errors.New("rule does not exist")
		}
	}
	return &bytes.Buffer{}, nil
}

// convergedHostState is an iptables-save snapshot that fully matches the desired host
// rules (the POSTROUTING jump + one SNAT rule in ISTIO_POSTRT).
const convergedHostState = `*nat
:PREROUTING ACCEPT [0:0]
:POSTROUTING ACCEPT [0:0]
:ISTIO_POSTRT - [0:0]
-A POSTROUTING -j ISTIO_POSTRT
-A ISTIO_POSTRT -m owner --socket-exists -p tcp -m set --match-set istio-inpod-probes-v4 dst -j SNAT --to-source 169.254.7.127
COMMIT
`

// driftedHostState simulates the state after an external flush: the ISTIO_POSTRT chain
// still exists but its rules are gone (the reproduction step of issue #60607,
// `iptables -t nat -F ISTIO_POSTRT`).
const driftedHostState = `*nat
:PREROUTING ACCEPT [0:0]
:POSTROUTING ACCEPT [0:0]
:ISTIO_POSTRT - [0:0]
-A POSTROUTING -j ISTIO_POSTRT
COMMIT
`

func TestEnsureHostRulesNoopWhenConverged(t *testing.T) {
	cfg := constructTestConfig()
	ext := &hostStateStub{saveOutput: convergedHostState}
	iptConfigurator, _, err := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
	assert.NoError(t, err)

	repaired, err := iptConfigurator.EnsureHostRules()
	assert.NoError(t, err)
	assert.Equal(t, false, repaired)

	// When converged this must be a pure read-only verification:
	// no delete/recreate/restore writes are allowed
	assert.Equal(t, 0, len(ext.ExecutedStdin))
	for _, cmd := range ext.ExecutedAll {
		for _, forbidden := range []string{" -D ", " -F ", " -X ", "iptables-restore"} {
			if strings.Contains(cmd, forbidden) {
				t.Fatalf("expected no mutating command when converged, got: %v", cmd)
			}
		}
	}
	// ip6tables must not be touched when IPv6 is disabled
	for _, cmd := range ext.ExecutedAll {
		if strings.Contains(cmd, "ip6tables") {
			t.Fatalf("expected no ip6tables invocation when IPv6 is disabled, got: %v", cmd)
		}
	}
}

func TestEnsureHostRulesRepairsDrift(t *testing.T) {
	cfg := constructTestConfig()
	ext := &hostStateStub{saveOutput: driftedHostState}
	iptConfigurator, _, err := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
	assert.NoError(t, err)

	repaired, err := iptConfigurator.EnsureHostRules()
	assert.NoError(t, err)
	assert.Equal(t, true, repaired)

	// The repair must mirror the startup path: delete first (-D jump / -F / -X chain),
	// then re-create (iptables-restore)
	joined := strings.Join(ext.ExecutedAll, "\n")
	for _, expected := range []string{
		"-t nat -D POSTROUTING -j ISTIO_POSTRT",
		"-t nat -F ISTIO_POSTRT",
		"-t nat -X ISTIO_POSTRT",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected delete command %q in executed commands:\n%v", expected, joined)
		}
	}
	// The restore payload (stdin) must re-create the SNAT rule
	restorePayload := strings.Join(ext.ExecutedStdin, "\n")
	if !strings.Contains(restorePayload, "-j SNAT --to-source 169.254.7.127") {
		t.Fatalf("expected SNAT rule in restore payload:\n%v", restorePayload)
	}

	// Guardrail DROP rules must never show up in the host netns
	// (they would drop the entire node's traffic)
	if strings.Contains(joined, "-j DROP") {
		t.Fatalf("host rules reconcile must never install guardrail DROP rules, got:\n%v", joined)
	}
}

func TestEnsureHostRulesSkipsRepairWhenStateUnreadable(t *testing.T) {
	// A transient iptables-save failure means the state could not be verified, NOT
	// that the rules have drifted. Deleting live, possibly-healthy rules based on a
	// failed read would let the reconciler itself cause the outage it exists to
	// prevent, so EnsureHostRules must surface the error and perform no writes.
	cfg := constructTestConfig()
	ext := &hostStateStub{saveErr: errors.New("iptables-save failed transiently")}
	iptConfigurator, _, err := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
	assert.NoError(t, err)

	repaired, err := iptConfigurator.EnsureHostRules()
	assert.Equal(t, false, repaired)
	if err == nil {
		t.Fatal("expected an error when the iptables state cannot be read")
	}

	// No mutating command may run when verification itself failed
	assert.Equal(t, 0, len(ext.ExecutedStdin))
	for _, cmd := range ext.ExecutedAll {
		for _, forbidden := range []string{" -D ", " -F ", " -X "} {
			if strings.Contains(cmd, forbidden) {
				t.Fatalf("expected no mutating command when state is unreadable, got: %v", cmd)
			}
		}
	}
}

// foreignChainHostState is convergedHostState plus an ISTIO_-prefixed chain that is not
// part of the expected host state (e.g. residue from another istio component or
// version). The delete+create repair cannot remove such a chain, so treating it as
// drift would make the reconcile loop re-apply the rules every tick forever.
const foreignChainHostState = `*nat
:PREROUTING ACCEPT [0:0]
:POSTROUTING ACCEPT [0:0]
:ISTIO_POSTRT - [0:0]
:ISTIO_OUTPUT - [0:0]
-A POSTROUTING -j ISTIO_POSTRT
-A ISTIO_POSTRT -m owner --socket-exists -p tcp -m set --match-set istio-inpod-probes-v4 dst -j SNAT --to-source 169.254.7.127
-A ISTIO_OUTPUT -p tcp -j ACCEPT
COMMIT
`

func TestEnsureHostRulesIgnoresForeignIstioChains(t *testing.T) {
	cfg := constructTestConfig()
	ext := &hostStateStub{saveOutput: foreignChainHostState}
	iptConfigurator, _, err := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
	assert.NoError(t, err)

	// All managed rules are present, so the foreign ISTIO_OUTPUT chain must not be
	// reported as drift and no repair may run
	repaired, err := iptConfigurator.EnsureHostRules()
	assert.NoError(t, err)
	assert.Equal(t, false, repaired)
	assert.Equal(t, 0, len(ext.ExecutedStdin))
}

func TestEnsureHostRulesRetriesFailedRepair(t *testing.T) {
	// Once the drifted rules have been deleted, a transiently failing re-create must
	// be retried immediately (mirroring the startup path), instead of leaving the
	// node without any probe SNAT rules until the next reconcile tick.
	cfg := constructTestConfig()
	ext := &hostStateStub{saveOutput: driftedHostState, restoreFailures: 1}
	iptConfigurator, _, err := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
	assert.NoError(t, err)

	repaired, err := iptConfigurator.EnsureHostRules()
	assert.NoError(t, err)
	assert.Equal(t, true, repaired)

	// The first restore failed, so the payload must have been re-sent at least twice
	snatAttempts := 0
	for _, line := range ext.ExecutedStdin {
		if strings.Contains(line, "-j SNAT --to-source 169.254.7.127") {
			snatAttempts++
		}
	}
	if snatAttempts < 2 {
		t.Fatalf("expected the repair to be retried after a failed restore, got %d restore attempts:\n%v",
			snatAttempts, strings.Join(ext.ExecutedStdin, "\n"))
	}
}

func TestEnsureHostRulesRepairsMissingCheckedRule(t *testing.T) {
	// Chains and rule counts look right, but the per-rule -C checks fail (e.g. the
	// POSTROUTING jump was removed externally and other rules were inserted instead);
	// this must also trigger a repair
	cfg := constructTestConfig()
	ext := &hostStateStub{saveOutput: convergedHostState, checkFails: true}
	iptConfigurator, _, err := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
	assert.NoError(t, err)

	repaired, err := iptConfigurator.EnsureHostRules()
	assert.NoError(t, err)
	assert.Equal(t, true, repaired)
}
