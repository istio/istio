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

package dependencies

import (
	"fmt"
	"os"
	"testing"

	utilversion "k8s.io/apimachinery/pkg/util/version"

	"istio.io/istio/pkg/test/util/assert"
)

func TestOverrideVersionIsCorrectlyParsed(t *testing.T) {
	cases := []struct {
		name string
		ver  string
		want *utilversion.Version
	}{
		{
			name: "jammy nft",
			ver:  "iptables v1.8.7 (nf_tables)",
			want: utilversion.MustParseGeneric("1.8.7"),
		},
		{
			name: "jammy legacy",
			ver:  "iptables v1.8.7 (legacy)",

			want: utilversion.MustParseGeneric("1.8.7"),
		},
		{
			name: "xenial",
			ver:  "iptables v1.6.0",

			want: utilversion.MustParseGeneric("1.6.0"),
		},
		{
			name: "bionic",
			ver:  "iptables v1.6.1",

			want: utilversion.MustParseGeneric("1.6.1"),
		},
		{
			name: "centos 7",
			ver:  "iptables v1.4.21",

			want: utilversion.MustParseGeneric("1.4.21"),
		},
		{
			name: "centos 8",
			ver:  "iptables v1.8.4 (nf_tables)",

			want: utilversion.MustParseGeneric("1.8.4"),
		},
		{
			name: "alpine 3.18",
			ver:  "iptables v1.8.9 (legacy)",

			want: utilversion.MustParseGeneric("1.8.9"),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIptablesVer(tt.ver)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, got.String(), tt.want.String())
		})
	}
}

func TestDetectIptablesVersion(t *testing.T) {
	cases := []struct {
		name            string
		shouldUseBinary func(string) (IptablesVersion, error)
		dep             *RealDependencies
		result          IptablesVersion
		expected        error
	}{
		{
			name: "FORCE_IPTABLES_BINARY_is_found",
			shouldUseBinary: func(s string) (IptablesVersion, error) {
				if s == iptablesNftBin {
					return IptablesVersion{DetectedBinary: iptablesNftBin}, nil
				}

				return IptablesVersion{}, fmt.Errorf("binary not found")
			},
			dep: &RealDependencies{
				ForceIptablesBinary: "nft",
			},
			result:   IptablesVersion{DetectedBinary: iptablesNftBin},
			expected: nil,
		},
		{
			name: "FORCE_IPTABLES_BINARY_not_found",
			shouldUseBinary: func(s string) (IptablesVersion, error) {
				return IptablesVersion{}, fmt.Errorf("binary not found")
			},
			dep: &RealDependencies{
				ForceIptablesBinary: "legacy",
			},
			result:   IptablesVersion{},
			expected: fmt.Errorf("binary not found"),
		},
		{
			name: "FORCE_IPTABLES_BINARY_not_valid",
			shouldUseBinary: func(s string) (IptablesVersion, error) {
				return IptablesVersion{}, fmt.Errorf("binary not found")
			},
			dep: &RealDependencies{
				ForceIptablesBinary: "iptables",
			},
			result:   IptablesVersion{},
			expected: fmt.Errorf("iptables binary %q unsupported", "iptables"),
		},
		{
			name: "selection_logic_finds_nft",
			shouldUseBinary: func(s string) (IptablesVersion, error) {
				if s == iptablesNftBin {
					return IptablesVersion{DetectedBinary: iptablesNftBin}, nil
				}

				return IptablesVersion{}, fmt.Errorf("binary not found")
			},
			dep:      &RealDependencies{},
			result:   IptablesVersion{DetectedBinary: iptablesNftBin},
			expected: nil,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			shouldUseBinaryForContext = tt.shouldUseBinary
			defer func() {
				shouldUseBinaryForContext = shouldUseBinaryForCurrentContext
			}()
			r, e := tt.dep.DetectIptablesVersion(false)
			assert.Equal(t, tt.result, r)
			assert.Equal(t, tt.expected, e)
		})
	}
}

// TestDetectIptablesVersionPersistence covers https://github.com/istio/istio/issues/61020:
// probing which binary/table to use is not side-effect-free (even a read-only probe
// materializes empty kernel tables), so re-running the "does legacy have existing rules"
// heuristic on every process restart can flip the detected backend and duplicate rules
// already written under the previously-detected one. Once a backend has been detected, it
// must be reused on subsequent calls within the same boot instead of re-probed.
func TestDetectIptablesVersionPersistence(t *testing.T) {
	detectedIptablesBackendDir = t.TempDir()
	defer func() {
		detectedIptablesBackendDir = "/var/run/istio-cni"
	}()

	t.Run("persists the detected backend and reuses it without re-probing", func(t *testing.T) {
		var calls []string
		shouldUseBinaryForContext = func(s string) (IptablesVersion, error) {
			calls = append(calls, s)
			if s == iptablesNftBin {
				return IptablesVersion{DetectedBinary: iptablesNftBin}, nil
			}
			// legacy binary exists, but (as in a clean boot) has no existing rules
			return IptablesVersion{DetectedBinary: iptablesLegacyBin}, nil
		}
		defer func() {
			shouldUseBinaryForContext = shouldUseBinaryForCurrentContext
		}()

		r := &RealDependencies{}

		// First call: no persisted state yet, runs the full legacy-then-nft heuristic.
		v, err := r.DetectIptablesVersion(false)
		assert.NoError(t, err)
		assert.Equal(t, IptablesVersion{DetectedBinary: iptablesNftBin}, v)
		assert.Equal(t, []string{iptablesLegacyBin, iptablesNftBin}, calls)

		persisted, err := os.ReadFile(detectedIptablesBackendFile(false))
		assert.NoError(t, err)
		assert.Equal(t, nft, string(persisted))

		// Second call simulates a process restart in the same boot: it must go straight to
		// re-validating the persisted binary, and must NOT re-run the existing-rules heuristic
		// against legacyBin (which is exactly what causes the flip in the reported bug).
		calls = nil
		v, err = r.DetectIptablesVersion(false)
		assert.NoError(t, err)
		assert.Equal(t, IptablesVersion{DetectedBinary: iptablesNftBin}, v)
		assert.Equal(t, []string{iptablesNftBin}, calls)
	})

	t.Run("falls back to full detection if the persisted backend no longer works", func(t *testing.T) {
		assert.NoError(t, os.MkdirAll(detectedIptablesBackendDir, 0o755))
		assert.NoError(t, os.WriteFile(detectedIptablesBackendFile(false), []byte(legacy), 0o644))

		var calls []string
		shouldUseBinaryForContext = func(s string) (IptablesVersion, error) {
			calls = append(calls, s)
			if s == iptablesLegacyBin {
				return IptablesVersion{}, fmt.Errorf("binary not found")
			}
			return IptablesVersion{DetectedBinary: iptablesNftBin}, nil
		}
		defer func() {
			shouldUseBinaryForContext = shouldUseBinaryForCurrentContext
		}()

		v, err := (&RealDependencies{}).DetectIptablesVersion(false)
		assert.NoError(t, err)
		assert.Equal(t, IptablesVersion{DetectedBinary: iptablesNftBin}, v)
		// re-validating the stale persisted "legacy" choice, then falling through to the
		// normal legacy-then-nft heuristic
		assert.Equal(t, []string{iptablesLegacyBin, iptablesLegacyBin, iptablesNftBin}, calls)
	})
}
