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

package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/tools/istio-clean-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

func constructTestConfig() *config.Config {
	return &config.Config{
		ProxyUID:           constants.DefaultProxyUID,
		ProxyGID:           constants.DefaultProxyUID,
		OwnerGroupsInclude: constants.OwnerGroupsInclude.DefaultValue,
	}
}

func TestIptables(t *testing.T) {
	cases := []struct {
		name   string
		config func(cfg *config.Config)
	}{
		{
			"empty",
			func(*config.Config) {},
		},
		{
			"dns",
			func(cfg *config.Config) {
				cfg.RedirectDNS = true
			},
		},
		{
			"dns-uid-gid",
			func(cfg *config.Config) {
				cfg.RedirectDNS = true
				cfg.DNSServersV4 = []string{"127.0.0.53"}
				cfg.DNSServersV6 = []string{"::127.0.0.53"}
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
			},
		},
		{
			"outbound-owner-groups",
			func(cfg *config.Config) {
				cfg.RedirectDNS = true
				cfg.OwnerGroupsInclude = "java,202"
			},
		},
		{
			"outbound-owner-groups-exclude",
			func(cfg *config.Config) {
				cfg.RedirectDNS = true
				cfg.OwnerGroupsExclude = "888,ftp"
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)

			ext := &DependenciesStub{}
			cleaner := NewIptablesCleaner(cfg, ext)

			cleaner.Run()

			compareToGolden(t, tt.name, ext.ExecutedAll)

			expectedExecutedNormally := []string{"iptables-save", "ip6tables-save"}
			if diff := cmp.Diff(ext.ExecutedNormally, expectedExecutedNormally); diff != "" {
				t.Fatalf("Executed normally commands: got\n%v\nwant\n%vdiff %v",
					ext.ExecutedNormally, expectedExecutedNormally, diff)
			}

			expectedExecutedQuietly := ext.ExecutedAll[:len(ext.ExecutedAll)-len(expectedExecutedNormally)]
			if diff := cmp.Diff(ext.ExecutedQuietly, expectedExecutedQuietly); diff != "" {
				t.Fatalf("Executed quietly commands: got\n%v\nwant\n%vdiff %v",
					ext.ExecutedQuietly, expectedExecutedQuietly, diff)
			}
		})
	}
}

func compareToGolden(t *testing.T, name string, actual []string) {
	t.Helper()
	gotBytes := []byte(strings.Join(actual, "\n"))
	goldenFile := filepath.Join("testdata", name+".golden")
	testutil.CompareContent(t, gotBytes, goldenFile)
}

type DependenciesStub struct {
	ExecutedNormally []string
	ExecutedQuietly  []string
	ExecutedAll      []string
}

func (s *DependenciesStub) RunOrFail(cmd string, args ...string) {
	s.execute(false /*quietly*/, cmd, args...)
}

func (s *DependenciesStub) Run(cmd string, args ...string) error {
	s.execute(false /*quietly*/, cmd, args...)
	return nil
}

func (s *DependenciesStub) RunQuietlyAndIgnore(cmd string, args ...string) {
	s.execute(true /*quietly*/, cmd, args...)
}

func (s *DependenciesStub) execute(quietly bool, cmd string, args ...string) {
	cmdline := strings.Join(append([]string{cmd}, args...), " ")
	s.ExecutedAll = append(s.ExecutedAll, cmdline)
	if quietly {
		s.ExecutedQuietly = append(s.ExecutedQuietly, cmdline)
	} else {
		s.ExecutedNormally = append(s.ExecutedNormally, cmdline)
	}
}
