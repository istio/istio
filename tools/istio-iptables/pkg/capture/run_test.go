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
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	// Create a new mount namespace.
	"github.com/howardjohn/unshare-go/mountns"
	// Create a new network namespace. This will have the 'lo' interface ready but nothing else.
	_ "github.com/howardjohn/unshare-go/netns"
	// Create a new user namespace. This will map the current UID/GID to 0.
	"github.com/howardjohn/unshare-go/userns"

	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func constructTestConfig() *config.Config {
	return &config.Config{
		ProxyPort:               "15001",
		InboundCapturePort:      "15006",
		InboundTunnelPort:       "15008",
		ProxyUID:                constants.DefaultProxyUID,
		ProxyGID:                constants.DefaultProxyUID,
		InboundTProxyMark:       "1337",
		InboundTProxyRouteTable: "133",
		OwnerGroupsInclude:      constants.OwnerGroupsInclude.DefaultValue,
		HostIPv4LoopbackCidr:    constants.HostIPv4LoopbackCidr.DefaultValue,
	}
}

func getCommonTestCases() []struct {
	name   string
	config func(cfg *config.Config)
} {
	return []struct {
		name   string
		config func(cfg *config.Config)
	}{
		{
			"ipv6-empty-inbound-ports",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = ""
				cfg.EnableIPv6 = true
			},
		},
		{
			"ip-range",
			func(cfg *config.Config) {
				cfg.OutboundIPRangesExclude = "1.1.0.0/16"
				cfg.OutboundIPRangesInclude = "9.9.0.0/16"
				cfg.DryRun = true
				cfg.RedirectDNS = true
				cfg.EnableIPv6 = false
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
				cfg.DNSServersV4 = []string{"127.0.0.53"}
			},
		},
		{
			"tproxy",
			func(cfg *config.Config) {
				cfg.InboundInterceptionMode = constants.TPROXY
				cfg.InboundPortsInclude = "*"
				cfg.OutboundIPRangesExclude = "1.1.0.0/16"
				cfg.OutboundIPRangesInclude = "9.9.0.0/16"
				cfg.DryRun = true
				cfg.RedirectDNS = true
				cfg.DNSServersV4 = []string{"127.0.0.53"}
				cfg.ProxyGID = "1337"
				cfg.ProxyUID = "1337"
				cfg.ExcludeInterfaces = "not-istio-nic"
				cfg.EnableIPv6 = true
			},
		},
		{
			"ipv6-inbound-ports",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "4000,5000"
				cfg.EnableIPv6 = true
			},
		},
		{
			"ipv6-virt-interfaces",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "4000,5000"
				cfg.RerouteVirtualInterfaces = "eth0,eth1"
				cfg.EnableIPv6 = true
			},
		},
		{
			"ipv6-ipnets",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "4000,5000"
				cfg.InboundPortsExclude = "6000,7000,"
				cfg.RerouteVirtualInterfaces = "eth0,eth1"
				cfg.OutboundIPRangesExclude = "2001:db8::/32"
				cfg.OutboundIPRangesInclude = "2001:db8::/32"
				cfg.EnableIPv6 = true
			},
		},
		{
			"ipv6-uid-gid",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "4000,5000"
				cfg.InboundPortsExclude = "6000,7000"
				cfg.RerouteVirtualInterfaces = "eth0,eth1"
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
				cfg.EnableIPv6 = true
				cfg.OutboundIPRangesExclude = "2001:db8::/32"
				cfg.OutboundIPRangesInclude = "2001:db8::/32"
			},
		},
		{
			"ipv6-outbound-ports",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = ""
				cfg.OutboundPortsInclude = "32000,31000"
				cfg.EnableIPv6 = true
			},
		},
		{
			"empty",
			func(cfg *config.Config) {},
		},
		{
			"kube-virt-interfaces",
			func(cfg *config.Config) {
				cfg.RerouteVirtualInterfaces = "eth1,eth2"
				cfg.OutboundIPRangesInclude = "*"
			},
		},
		{
			"ipnets",
			func(cfg *config.Config) {
				cfg.OutboundIPRangesInclude = "10.0.0.0/8"
			},
		},
		{
			"ipnets-with-kube-virt-interfaces",
			func(cfg *config.Config) {
				cfg.RerouteVirtualInterfaces = "eth1,eth2"
				cfg.OutboundIPRangesInclude = "10.0.0.0/8"
			},
		},
		{
			"inbound-ports-include",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "32000,31000"
			},
		},
		{
			"inbound-ports-wildcard",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "*"
			},
		},
		{
			"inbound-ports-tproxy",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "32000,31000"
				cfg.InboundInterceptionMode = constants.TPROXY
			},
		},
		{
			"inbound-ports-wildcard-tproxy",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "*"
				cfg.InboundInterceptionMode = constants.TPROXY
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
				cfg.EnableIPv6 = true
			},
		},
		{
			"ipv6-dns-uid-gid",
			func(cfg *config.Config) {
				cfg.EnableIPv6 = true
				cfg.RedirectDNS = true
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
			},
		},
		{
			"outbound-owner-groups",
			func(cfg *config.Config) {
				cfg.OwnerGroupsInclude = "java,202"
			},
		},
		{
			"outbound-owner-groups-exclude",
			func(cfg *config.Config) {
				cfg.OwnerGroupsExclude = "888,ftp"
			},
		},
		{
			"ipv6-dns-outbound-owner-groups",
			func(cfg *config.Config) {
				cfg.EnableIPv6 = true
				cfg.RedirectDNS = true
				cfg.OwnerGroupsInclude = "java,202"
			},
		},
		{
			"ipv6-dns-outbound-owner-groups-exclude",
			func(cfg *config.Config) {
				cfg.EnableIPv6 = true
				cfg.RedirectDNS = true
				cfg.OwnerGroupsExclude = "888,ftp"
			},
		},
		{
			"outbound-ports-include",
			func(cfg *config.Config) {
				cfg.OutboundPortsInclude = "32000,31000"
			},
		},
		{
			"loopback-outbound-iprange",
			func(cfg *config.Config) {
				cfg.OutboundIPRangesInclude = "127.1.2.3/32"
				cfg.DryRun = true
				cfg.RedirectDNS = true
				cfg.DNSServersV4 = []string{"127.0.0.53"}
				cfg.EnableIPv6 = false
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
			},
		},
		{
			"basic-exclude-nic",
			func(cfg *config.Config) {
				cfg.ExcludeInterfaces = "not-istio-nic"
			},
		},
		{
			"logging",
			func(cfg *config.Config) {
				cfg.TraceLogging = true
			},
		},
		{
			"drop-invalid",
			func(cfg *config.Config) {
				cfg.DropInvalid = true
			},
		},
		{
			"host-ipv4-loopback-cidr",
			func(cfg *config.Config) {
				cfg.HostIPv4LoopbackCidr = "127.0.0.1/8"
			},
		},
	}
}

func TestIptables(t *testing.T) {
	for _, tt := range getCommonTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)

			ext := &dep.DependenciesStub{}
			iptConfigurator := NewIptablesConfigurator(cfg, ext)
			iptConfigurator.Run()
			compareToGolden(t, tt.name, ext.ExecutedAll)
		})
	}
}

func TestSeparateV4V6(t *testing.T) {
	mkIPList := func(ips ...string) []netip.Prefix {
		ret := []netip.Prefix{}
		for _, ip := range ips {
			ipp, err := netip.ParsePrefix(ip)
			if err != nil {
				panic(err.Error())
			}
			ret = append(ret, ipp)
		}
		return ret
	}
	cases := []struct {
		name string
		cidr string
		v4   NetworkRange
		v6   NetworkRange
	}{
		{
			name: "wildcard",
			cidr: "*",
			v4:   NetworkRange{IsWildcard: true},
			v6:   NetworkRange{IsWildcard: true},
		},
		{
			name: "v4 only",
			cidr: "10.0.0.0/8,172.16.0.0/16",
			v4:   NetworkRange{CIDRs: mkIPList("10.0.0.0/8", "172.16.0.0/16")},
		},
		{
			name: "v6 only",
			cidr: "fd04:3e42:4a4e:3381::/64,ffff:ffff:ac10:ac10::/64",
			v6:   NetworkRange{CIDRs: mkIPList("fd04:3e42:4a4e:3381::/64", "ffff:ffff:ac10:ac10::/64")},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			iptConfigurator := NewIptablesConfigurator(cfg, &dep.DependenciesStub{})
			v4Range, v6Range, err := iptConfigurator.separateV4V6(tt.cidr)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(v4Range, tt.v4) {
				t.Fatalf("expected %v, got %v", tt.v4, v4Range)
			}
			if !reflect.DeepEqual(v6Range, tt.v6) {
				t.Fatalf("expected %v, got %v", tt.v6, v6Range)
			}
		})
	}
}

func TestIdempotentEquivalentRerun(t *testing.T) {
	setup(t)
	commonCases := getCommonTestCases()
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
				iptConfigurator := NewIptablesConfigurator(cfg, ext)
				assert.NoError(t, iptConfigurator.Run())
				residueExists, deltaExists := iptConfigurator.VerifyIptablesState(&iptVer, &ipt6Ver)
				assert.Equal(t, residueExists, false)
				assert.Equal(t, deltaExists, true)
			}()

			// First Pass
			cfg.Reconcile = false
			iptConfigurator := NewIptablesConfigurator(cfg, ext)
			assert.NoError(t, iptConfigurator.Run())
			residueExists, deltaExists := iptConfigurator.VerifyIptablesState(&iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			// Second Pass
			iptConfigurator = NewIptablesConfigurator(cfg, ext)
			assert.NoError(t, iptConfigurator.Run())

			// Execution should fail if force-apply is used and chains exists
			cfg.ForceApply = true
			iptConfigurator = NewIptablesConfigurator(cfg, ext)
			assert.Error(t, iptConfigurator.Run())
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
		// Put a new file we have permission to access over xtables.lock
		xtables := filepath.Join(t.TempDir(), "xtables.lock")
		_, err := os.Create(xtables)
		assert.NoError(t, err)
		_ = os.Mkdir("/run", 0o777)
		_ = mountns.BindMount(xtables, "/run/xtables.lock")
	})
}

func TestIdempotentUnequaledRerun(t *testing.T) {
	setup(t)
	commonCases := getCommonTestCases()
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

			defer func() {
				// Final Cleanup
				cfg.CleanupOnly = true
				cfg.Reconcile = false
				iptConfigurator := NewIptablesConfigurator(cfg, ext)
				assert.NoError(t, iptConfigurator.Run())
				residueExists, deltaExists := iptConfigurator.VerifyIptablesState(&iptVer, &ipt6Ver)
				assert.Equal(t, residueExists, true) // residue found due to extra OUTPUT rule
				assert.Equal(t, deltaExists, true)
				// Remove additional rule
				cmd := exec.Command("iptables", "-t", "nat", "-D", "OUTPUT", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
				}
				residueExists, deltaExists = iptConfigurator.VerifyIptablesState(&iptVer, &ipt6Ver)
				assert.Equal(t, residueExists, false)
				assert.Equal(t, deltaExists, true)
			}()

			// First Pass
			iptConfigurator := NewIptablesConfigurator(cfg, ext)
			assert.NoError(t, iptConfigurator.Run())
			residueExists, deltaExists := iptConfigurator.VerifyIptablesState(&iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			// Diverge from installation
			cmd := exec.Command("iptables", "-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			// Apply not required after tainting non-ISTIO chains with extra rules
			residueExists, deltaExists = iptConfigurator.VerifyIptablesState(&iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, false)

			cmd = exec.Command("iptables", "-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "123", "-j", "ACCEPT")
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Errorf("iptables cmd (%s %s) failed: %s", cmd.Path, cmd.Args, stderr.String())
			}

			// Apply required after tainting ISTIO chains
			residueExists, deltaExists = iptConfigurator.VerifyIptablesState(&iptVer, &ipt6Ver)
			assert.Equal(t, residueExists, true)
			assert.Equal(t, deltaExists, true)

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

func compareToGolden(t *testing.T, name string, actual []string) {
	t.Helper()
	gotBytes := []byte(strings.Join(actual, "\n"))
	goldenFile := filepath.Join("testdata", name+".golden")
	testutil.CompareContent(t, gotBytes, goldenFile)
}
