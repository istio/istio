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

package addressset

import (
	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/cni/pkg/nftables"
)

// New creates a new AddressSetManager instance based on the useNftables flag.
// If useNftables is false, it returns an ipset-based implementation.
// If useNftables is true, it returns an nftables-based implementation.
func New(useNftables bool, enableIPv6 bool) (AddressSetManager, error) {
	if useNftables {
		return nftables.NewHostSetManager(config.ProbeIPSet, enableIPv6)
	}

	// Use existing ipset implementation
	linDeps := ipset.RealNlDeps()
	ipsetInstance, err := ipset.NewIPSet(config.ProbeIPSet, enableIPv6, linDeps)
	if err != nil {
		return nil, err
	}

	return &IPSetWrapper{ipset: ipsetInstance}, nil
}
