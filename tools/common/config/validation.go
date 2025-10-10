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
	"fmt"
	"net/netip"
)

const (
	// Due to implementation constraints, we have to impose a limit on the
	// number of owner groups whose outgoing traffic should be redirected
	// to Envoy.
	//
	// Since all included groups will be translated into a single Iptables
	// rule that combines N match expressions `-m owner ! --gid-owner <GID>`,
	// we need to be sure it won't be too long.
	//
	// Most common Linux distributions allow no more than 128-1200
	// match expressions per rule.
	maxOwnerGroupsInclude = 64
)

func ValidateOwnerGroups(include, exclude string) error {
	filter := ParseInterceptFilter(include, exclude)
	if !filter.Except && len(filter.Values) > maxOwnerGroupsInclude {
		return fmt.Errorf("number of owner groups whose outgoing traffic "+
			"should be redirected to Envoy cannot exceed %d, got %d: %v",
			maxOwnerGroupsInclude, len(filter.Values), filter.Values)
	}
	return nil
}

func ValidateIPv4LoopbackCidr(cidr string) error {
	ipp, err := netip.ParsePrefix(cidr)
	if err != nil {
		return fmt.Errorf("failed to parse CIDR %s: %v", cidr, err)
	}

	if !ipp.Addr().Is4() || !ipp.Addr().IsLoopback() {
		return fmt.Errorf("expected valid IPv4 loopback address in CIDR %s; found %v", cidr, ipp.Addr())
	}

	ones := ipp.Bits()
	if ones < 8 || ones > 32 {
		return fmt.Errorf("expected CIDR %s to have mask in range [8, 32]; found %v", cidr, ones)
	}
	return nil
}

func ValidateNftLegacyFlags(nativeNftables, forceLegacyIPTables bool) error {
	if nativeNftables && forceLegacyIPTables {
		return fmt.Errorf("cannot set both NativeNftables and ForceLegacyIPTables, choose one")
	}

	return nil
}
