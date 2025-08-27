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
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"sigs.k8s.io/knftables"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/scopes"
	testutil "istio.io/istio/pilot/test/util"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	"istio.io/istio/tools/istio-nftables/pkg/builder"
)

// MockNftablesCapture wraps the MockNftables to capture all executed transactions
type MockNftablesCapture struct {
	*builder.MockNftables
	lock         sync.Mutex
	transactions []string
}

func NewMockNftablesCapture() *MockNftablesCapture {
	return &MockNftablesCapture{
		MockNftables: builder.NewMockNftables("", ""),
		transactions: make([]string, 0),
	}
}

func (m *MockNftablesCapture) Run(ctx context.Context, tx *knftables.Transaction) error {
	// Lets store the transaction as string before running it using the actual mock
	m.lock.Lock()
	if tx.NumOperations() > 0 {
		m.transactions = append(m.transactions, tx.String())
	}
	m.lock.Unlock()

	// Run the actual transaction through the mock
	return m.MockNftables.Run(ctx, tx)
}

func (m *MockNftablesCapture) GetCapturedRules() []string {
	m.lock.Lock()
	defer m.lock.Unlock()
	// Return a copy to avoid race conditions
	result := make([]string, len(m.transactions))
	copy(result, m.transactions)
	return result
}

func TestNftablesPodOverrides(t *testing.T) {
	cases := GetCommonInPodTestCases()

	for _, tt := range cases {
		for _, ipv6 := range []bool{false, true} {
			t.Run(tt.name+"_"+ipstr(ipv6), func(t *testing.T) {
				cfg := constructTestConfig()
				cfg.EnableIPv6 = ipv6
				tt.config(cfg)
				ext := &dep.DependenciesStub{}

				mock := NewMockNftablesCapture()
				originalProvider := nftProviderVar
				nftProviderVar = func(_ knftables.Family, table string) (builder.NftablesAPI, error) {
					return mock, nil
				}
				defer func() {
					nftProviderVar = originalProvider
				}()

				iptConfigurator, _, _ := NewNftablesConfigurator(cfg, cfg, ext, ext, iptables.EmptyNlDeps())
				err := iptConfigurator.CreateInpodRules(scopes.CNIAgent, tt.podOverrides)
				if err != nil {
					t.Fatal(err)
				}

				compareToGolden(t, ipv6, tt.name, mock.GetCapturedRules())
			})
		}
	}
}

func TestNftablesHostRules(t *testing.T) {
	cases := GetCommonHostTestCases()

	for _, tt := range cases {
		for _, ipv6 := range []bool{false, true} {
			t.Run(tt.name+"_"+ipstr(ipv6), func(t *testing.T) {
				cfg := constructTestConfig()
				cfg.EnableIPv6 = ipv6
				tt.config(cfg)
				ext := &dep.DependenciesStub{}

				mock := NewMockNftablesCapture()
				originalProvider := nftProviderVar
				nftProviderVar = func(_ knftables.Family, table string) (builder.NftablesAPI, error) {
					return mock, nil
				}
				defer func() {
					nftProviderVar = originalProvider
				}()

				iptConfigurator, _, _ := NewNftablesConfigurator(cfg, cfg, ext, ext, iptables.EmptyNlDeps())
				err := iptConfigurator.CreateHostRulesForHealthChecks()
				if err != nil {
					t.Fatal(err)
				}

				compareToGolden(t, ipv6, tt.name, mock.GetCapturedRules())
			})
		}
	}
}

func TestInvokedTwiceIsIdempotent(t *testing.T) {
	tests := GetCommonInPodTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)
			ext := &dep.DependenciesStub{}

			mock := NewMockNftablesCapture()
			originalProvider := nftProviderVar
			nftProviderVar = func(_ knftables.Family, table string) (builder.NftablesAPI, error) {
				return mock, nil
			}
			defer func() {
				nftProviderVar = originalProvider
			}()

			iptConfigurator, _, _ := NewNftablesConfigurator(cfg, cfg, ext, ext, iptables.EmptyNlDeps())
			err := iptConfigurator.CreateInpodRules(scopes.CNIAgent, tt.podOverrides)
			if err != nil {
				t.Fatal(err)
			}
			compareToGolden(t, false, tt.name, mock.GetCapturedRules())

			// Reset the mock to capture only the second run
			mock = NewMockNftablesCapture()
			nftProviderVar = func(_ knftables.Family, table string) (builder.NftablesAPI, error) {
				return mock, nil
			}

			// run another time to make sure its idempotent
			err = iptConfigurator.CreateInpodRules(scopes.CNIAgent, tt.podOverrides)
			if err != nil {
				t.Fatal(err)
			}
			compareToGolden(t, false, tt.name, mock.GetCapturedRules())
		})
	}
}

func ipstr(ipv6 bool) string {
	if ipv6 {
		return "ipv6"
	}
	return "ipv4"
}

func compareToGolden(t *testing.T, ipv6 bool, name string, actual []string) {
	t.Helper()
	gotBytes := []byte(strings.Join(actual, "\n"))
	goldenFile := filepath.Join("testdata", name+".golden")
	if ipv6 {
		goldenFile = filepath.Join("testdata", name+"_ipv6.golden")
	}
	testutil.CompareContent(t, gotBytes, goldenFile)
}

func constructTestConfig() *config.AmbientConfig {
	probeSNATipv4 := netip.MustParseAddr("169.254.7.127")
	probeSNATipv6 := netip.MustParseAddr("e9ac:1e77:90ca:399f:4d6d:ece2:2f9b:3164")
	return &config.AmbientConfig{
		HostProbeSNATAddress:   probeSNATipv4,
		HostProbeV6SNATAddress: probeSNATipv6,
	}
}
