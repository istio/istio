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

	"istio.io/api/networking/v1alpha3"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
)

const (
	IPAutoallocateStatusType = "ip-autoallocate"
)

func GetV2AddressesFromServiceEntry(se *networkingv1alpha3.ServiceEntry) []netip.Addr {
	if se == nil {
		return []netip.Addr{}
	}
	return getV2AddressesFromServiceEntryStatus(&se.Status)
}

func GetV2AddressesFromConfig(cfg config.Config) []netip.Addr {
	status, ok := cfg.Status.(*v1alpha3.ServiceEntryStatus)
	if !ok {
		return []netip.Addr{}
	}
	return getV2AddressesFromServiceEntryStatus(status)
}

func getV2AddressesFromServiceEntryStatus(status *v1alpha3.ServiceEntryStatus) []netip.Addr {
	results := []netip.Addr{}
	for _, addr := range status.GetAddresses() {
		parsed, err := netip.ParseAddr(addr.GetValue())
		if err != nil {
			// strange, we should have written these so it probaby should parse but for now unreadable is unusable and we move on
			continue
		}
		results = append(results, parsed)
	}
	return results
}

func ShouldV2AutoAllocateIP(se *networkingv1alpha3.ServiceEntry) bool {
	if se == nil {
		return false
	}
	return shouldV2AutoAllocateIPFromPieces(se.ObjectMeta, &se.Spec)
}

func ShouldV2AutoAllocateIPFromConfig(cfg config.Config) bool {
	spec, ok := cfg.Spec.(*v1alpha3.ServiceEntry)
	if !ok {
		return false
	}
	return shouldV2AutoAllocateIPFromPieces(cfg.ToObjectMeta(), spec)
}

func shouldV2AutoAllocateIPFromPieces(meta v1.ObjectMeta, spec *v1alpha3.ServiceEntry) bool {
	// if the feature is off we should not assign/use addresses
	if !features.EnableIPAutoallocate {
		return false
	}

	// if resolution is none we cannot honor the assigned IP in the dataplane and should not assign
	if spec.Resolution == v1alpha3.ServiceEntry_NONE {
		return false
	}

	// check for opt-out by user
	diabledValue, diabledFound := meta.Labels[constants.DisableV2AutoAllocationLabel]
	if diabledFound && strings.EqualFold(diabledValue, "true") {
		return false
	}

	// if the user assigned their own we don't alloate or use autoassigned addresses
	if len(spec.Addresses) > 0 {
		return false
	}

	return true
}
