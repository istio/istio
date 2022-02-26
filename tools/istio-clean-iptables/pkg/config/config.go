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

	types "istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/pkg/log"
)

// Command line options
// nolint: maligned
type Config struct {
	DryRun             bool     `json:"DRY_RUN"`
	ProxyUID           string   `json:"PROXY_UID"`
	ProxyGID           string   `json:"PROXY_GID"`
	RedirectDNS        bool     `json:"REDIRECT_DNS"`
	DNSServersV4       []string `json:"DNS_SERVERS_V4"`
	DNSServersV6       []string `json:"DNS_SERVERS_V6"`
	CaptureAllDNS      bool     `json:"CAPTURE_ALL_DNS"`
	OwnerGroupsInclude string   `json:"OUTBOUND_OWNER_GROUPS_INCLUDE"`
	OwnerGroupsExclude string   `json:"OUTBOUND_OWNER_GROUPS_EXCLUDE"`
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
