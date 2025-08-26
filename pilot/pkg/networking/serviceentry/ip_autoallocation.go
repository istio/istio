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

package serviceentry

import (
	"net/netip"
	"strings"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	apiv1 "istio.io/api/networking/v1"
	networkingv1 "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
)

func GetHostAddressesFromServiceEntry(se *networkingv1.ServiceEntry) map[string][]netip.Addr {
	if se == nil {
		return map[string][]netip.Addr{}
	}
	return getHostAddressesFromServiceEntryStatus(&se.Status)
}

func GetAddressesFromServiceEntry(se *networkingv1.ServiceEntry) []netip.Addr {
	addresses := []netip.Addr{}
	for _, v := range GetHostAddressesFromServiceEntry(se) {
		addresses = append(addresses, v...)
	}
	return addresses
}

func GetHostAddressesFromConfig(cfg config.Config) map[string][]netip.Addr {
	status, ok := cfg.Status.(*apiv1.ServiceEntryStatus)
	if !ok {
		return map[string][]netip.Addr{}
	}
	return getHostAddressesFromServiceEntryStatus(status)
}

func getHostAddressesFromServiceEntryStatus(status *apiv1.ServiceEntryStatus) map[string][]netip.Addr {
	results := map[string][]netip.Addr{}
	for _, addr := range status.GetAddresses() {
		parsed, err := netip.ParseAddr(addr.GetValue())
		if err != nil {
			// strange, we should have written these so it probably should parse but for now unreadable is unusable and we move on
			continue
		}
		host := addr.GetHost()
		results[host] = append(results[host], parsed)
	}
	return results
}

func ShouldV2AutoAllocateIP(se *networkingv1.ServiceEntry) bool {
	if se == nil {
		return false
	}
	return shouldV2AutoAllocateIPFromPieces(se.ObjectMeta, &se.Spec)
}

func ShouldV2AutoAllocateIPFromConfig(cfg config.Config) bool {
	spec, ok := cfg.Spec.(*apiv1.ServiceEntry)
	if !ok {
		return false
	}
	return shouldV2AutoAllocateIPFromPieces(cfg.ToObjectMeta(), spec)
}

func shouldV2AutoAllocateIPFromPieces(meta v1.ObjectMeta, spec *apiv1.ServiceEntry) bool {
	// if the feature is off we should not assign/use addresses
	if !features.EnableIPAutoallocate {
		return false
	}

	// if resolution is none we cannot honor the assigned IP in the dataplane and should not assign
	if spec.Resolution == apiv1.ServiceEntry_NONE {
		return false
	}

	// check for opt-out by user
	enabledValue, enabledFound := meta.Labels[label.NetworkingEnableAutoallocateIp.Name]
	if enabledFound && strings.EqualFold(enabledValue, "false") {
		return false
	}

	// if the user assigned their own we don't allocate or use autoassigned addresses
	if len(spec.Addresses) > 0 {
		return false
	}

	return true
}
