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
	"encoding/json"
	"net/netip"

	"istio.io/api/meta/v1alpha1"
	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
)

const (
	IPAutoallocateStatusType = "ip-autoallocate"
)

func GetV2AddressesFromServiceEntry(se *networkingv1alpha3.ServiceEntry) []netip.Addr {
	if se == nil {
		return []netip.Addr{}
	}
	return kludgeFromStatus(se.Status.GetConditions())
}

func ShouldV2AutoAllocateIP(se *networkingv1alpha3.ServiceEntry) bool {
	// if the feature is off we should not assign/use addresses
	if !features.EnableV2IPAutoallocate {
		return false
	}
	if se == nil {
		return false
	}

	// check for opt-out by user
	// TODO: namespace opt-out?
	disabledValue, disabledFound := se.Labels[constants.DisableV2AutoAllocationLabel]
	if disabledFound && disabledValue == "true" {
		// user did not opt in
		return false
	}

	// if the user assigned their own we don't alloate or use autoassigned addresses
	if len(se.Spec.Addresses) > 0 {
		return false
	}

	return true
}

type ServiceEntryStatusKludge struct {
	Addresses []string // json:"addresses"
}

func kludgeFromStatus(conditions []*v1alpha1.IstioCondition) []netip.Addr {
	result := []netip.Addr{}
	for _, c := range conditions {
		if c == nil {
			continue
		}
		if c.Type != IPAutoallocateStatusType {
			continue
		}
		jsonAddresses := c.Message
		kludge := ServiceEntryStatusKludge{}
		err := json.Unmarshal([]byte(jsonAddresses), &kludge)
		if err != nil {
			continue
		}
		for _, address := range kludge.Addresses {
			result = append(result, netip.MustParseAddr(address))
		}
	}
	return result
}

func ConditionKludge(input []netip.Addr) v1alpha1.IstioCondition {
	addresses := []string{}
	for _, addr := range input {
		addresses = append(addresses, addr.String())
	}
	kludge := ServiceEntryStatusKludge{
		Addresses: addresses,
	}
	result, _ := json.Marshal(kludge)
	return v1alpha1.IstioCondition{
		Type:    IPAutoallocateStatusType,
		Status:  "true",
		Reason:  "AutoAllocatedAddress",
		Message: string(result),
	}
}
