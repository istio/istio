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

package config

import (
	"encoding/json"
	"fmt"
	"os/user"

	"github.com/miekg/dns"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	netutil "istio.io/istio/pkg/util/net"
	types "istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

func DefaultConfig() *Config {
	return &Config{
		OwnerGroupsInclude: constants.OwnerGroupsInclude.DefaultValue,
		OwnerGroupsExclude: constants.OwnerGroupsExclude.DefaultValue,
	}
}

// Command line options
// nolint: maligned
type Config struct {
	DryRun                  bool     `json:"DRY_RUN"`
	ProxyUID                string   `json:"PROXY_UID"`
	ProxyGID                string   `json:"PROXY_GID"`
	RedirectDNS             bool     `json:"REDIRECT_DNS"`
	DNSServersV4            []string `json:"DNS_SERVERS_V4"`
	DNSServersV6            []string `json:"DNS_SERVERS_V6"`
	CaptureAllDNS           bool     `json:"CAPTURE_ALL_DNS"`
	OwnerGroupsInclude      string   `json:"OUTBOUND_OWNER_GROUPS_INCLUDE"`
	OwnerGroupsExclude      string   `json:"OUTBOUND_OWNER_GROUPS_EXCLUDE"`
	InboundInterceptionMode string   `json:"INBOUND_INTERCEPTION_MODE"`
	InboundTProxyMark       string   `json:"INBOUND_TPROXY_MARK"`
}

func (c *Config) String() string {
	output, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		log.Fatalf("Unable to marshal config object: %v", err)
	}
	return string(output)
}

func (c *Config) Print() {
	fmt.Println("Variables:")
	fmt.Println("----------")
	fmt.Printf("PROXY_UID=%s\n", c.ProxyUID)
	fmt.Printf("PROXY_GID=%s\n", c.ProxyGID)
	fmt.Printf("DNS_CAPTURE=%t\n", c.RedirectDNS)
	fmt.Printf("CAPTURE_ALL_DNS=%t\n", c.CaptureAllDNS)
	fmt.Printf("DNS_SERVERS=%s,%s\n", c.DNSServersV4, c.DNSServersV6)
	fmt.Printf("OUTBOUND_OWNER_GROUPS_INCLUDE=%s\n", c.OwnerGroupsInclude)
	fmt.Printf("OUTBOUND_OWNER_GROUPS_EXCLUDE=%s\n", c.OwnerGroupsExclude)
	fmt.Println("")
}

func (c *Config) Validate() error {
	return types.ValidateOwnerGroups(c.OwnerGroupsInclude, c.OwnerGroupsExclude)
}

var envoyUserVar = env.Register(constants.EnvoyUser, "istio-proxy", "Envoy proxy username")

func (c *Config) FillConfigFromEnvironment() {
	// Fill in env-var only options
	c.OwnerGroupsInclude = constants.OwnerGroupsInclude.Get()
	c.OwnerGroupsExclude = constants.OwnerGroupsExclude.Get()
	c.InboundInterceptionMode = constants.IstioInboundInterceptionMode.Get()
	c.InboundTProxyMark = constants.IstioInboundTproxyMark.Get()
	// TODO: Make this more configurable, maybe with an allowlist of users to be captured for output instead of a denylist.
	if c.ProxyUID == "" {
		usr, err := user.Lookup(envoyUserVar.Get())
		var userID string
		// Default to the UID of ENVOY_USER
		if err != nil {
			userID = constants.DefaultProxyUID
		} else {
			userID = usr.Uid
		}
		c.ProxyUID = userID
	}

	// For TPROXY as its uid and gid are same.
	if c.ProxyGID == "" {
		c.ProxyGID = c.ProxyUID
	}
	// Lookup DNS nameservers. We only do this if DNS is enabled in case of some obscure theoretical
	// case where reading /etc/resolv.conf could fail.
	// If capture all DNS option is enabled, we don't need to read from the dns resolve conf. All
	// traffic to port 53 will be captured.
	if c.RedirectDNS && !c.CaptureAllDNS {
		dnsConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			log.Fatalf("failed to load /etc/resolv.conf: %v", err)
		}
		c.DNSServersV4, c.DNSServersV6 = netutil.IPsSplitV4V6(dnsConfig.Servers)
	}
}
