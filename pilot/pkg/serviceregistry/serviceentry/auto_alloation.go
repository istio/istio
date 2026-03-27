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
	"fmt"
	"hash/fnv"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/kind"
)

var (
	prime  = 65011     // Used for secondary hash function.
	maxIPs = 256 * 254 // Maximum possible IPs for address allocation.
)

type octetPair struct {
	thirdOctet  int
	fourthOctet int
}

// Automatically allocates IPs for service entry services WITHOUT an
// address field if the hostname is not a wildcard, or when resolution
// is not NONE. The IPs are allocated from the reserved Class E subnet
// (240.240.0.0/16) that is not reachable outside the pod or reserved
// Benchmarking IP range (2001:2::/48) in RFC5180. When DNS
// capture is enabled, Envoy will resolve the DNS to these IPs. The
// listeners for TCP services will also be set up on these IPs. The
// IPs allocated to a service entry may differ from istiod to istiod
// but it does not matter because these IPs only affect the listener
// IPs on a given proxy managed by a given istiod.
//
// NOTE: If DNS capture is not enabled by the proxy, the automatically
// allocated IP addresses do not take effect.
//
// The current algorithm to allocate IPs is deterministic across all istiods.
func autoAllocateIPs(services []*model.Service) []*model.Service {
	// if we are using the IP Autoallocate controller then we can short circuit this
	if features.EnableIPAutoallocate {
		return services
	}
	hashedServices := make([]*model.Service, maxIPs)
	hash := fnv.New32a()
	// First iterate through the range of services and determine its position by hash
	// so that we can deterministically allocate an IP.
	// We use "Double Hashning" for collision detection.
	// The hash algorithm is
	// - h1(k) = Sum32 hash of the service key (namespace + "/" + hostname)
	// - Check if we have an empty slot for h1(x) % MAXIPS. Use it if available.
	// - If there is a collision, apply second hash i.e. h2(x) = PRIME - (Key % PRIME)
	//   where PRIME is the max prime number below MAXIPS.
	// - Calculate new hash iteratively till we find an empty slot with (h1(k) + i*h2(k)) % MAXIPS
	j := 0
	for _, svc := range services {
		// we can allocate IPs only if
		// 1. the service has resolution set to static/dns. We cannot allocate
		//   for NONE because we will not know the original DST IP that the application requested.
		// 2. the address is not set (0.0.0.0)
		// 3. the hostname is not a wildcard
		if svc.DefaultAddress == constants.UnspecifiedIP && !svc.Hostname.IsWildCarded() && svc.Resolution != model.Passthrough {
			if j >= maxIPs {
				log.Errorf("out of IPs to allocate for service entries. maxips:= %d", maxIPs)
				break
			}
			// First hash is calculated by hashing the service key i.e. (namespace + "/" + hostname).
			hash.Write([]byte(svc.Key()))
			s := hash.Sum32()
			firstHash := s % uint32(maxIPs)
			// Check if there is a service with this hash first. If there is no service
			// at this location - then we can safely assign this position for this service.
			if hashedServices[firstHash] == nil {
				hashedServices[firstHash] = svc
			} else {
				// This means we have a collision. Resolve collision by "DoubleHashing".
				i := uint32(1)
				secondHash := uint32(prime) - (s % uint32(prime))
				for {
					nh := (s + i*secondHash) % uint32(maxIPs-1)
					if hashedServices[nh] == nil {
						hashedServices[nh] = svc
						break
					}
					i++
				}
			}
			hash.Reset()
			j++
		}
	}

	x := 0
	hnMap := make(map[string]octetPair)
	for _, svc := range hashedServices {
		x++
		if svc == nil {
			// There is no service in the slot. Just increment x and move forward.
			continue
		}
		n := svc.Key()
		// To avoid allocating 240.240.(i).255, if X % 255 is 0, increment X.
		// For example, when X=510, the resulting IP would be 240.240.2.0 (invalid)
		// So we bump X to 511, so that the resulting IP is 240.240.2.1
		if x%255 == 0 {
			x++
		}
		if v, ok := hnMap[n]; ok {
			log.Debugf("Reuse IP for domain %s", n)
			setAutoAllocatedIPs(svc, v)
		} else {
			thirdOctet := x / 255
			fourthOctet := x % 255
			pair := octetPair{thirdOctet, fourthOctet}
			setAutoAllocatedIPs(svc, pair)
			hnMap[n] = pair
		}
	}
	return services
}

func setAutoAllocatedIPs(svc *model.Service, octets octetPair) {
	a := octets.thirdOctet
	b := octets.fourthOctet
	svc.AutoAllocatedIPv4Address = fmt.Sprintf("240.240.%d.%d", a, b)
	if a == 0 {
		svc.AutoAllocatedIPv6Address = fmt.Sprintf("2001:2::f0f0:%x", b)
	} else {
		svc.AutoAllocatedIPv6Address = fmt.Sprintf("2001:2::f0f0:%x%x", a, b)
	}
}

func makeConfigKey(svc *model.Service) model.ConfigKey {
	return model.ConfigKey{
		Kind:      kind.ServiceEntry,
		Name:      string(svc.Hostname),
		Namespace: svc.Attributes.Namespace,
	}
}
