// Copyright 2017 Istio Authors
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
	"sort"
	"strings"
)

// Hostnames is a collection of Hostname; it exists so it's easy to sort hostnames consistently across Pilot.
// In a few locations we care about the order hostnames appear in Envoy config: primarily HTTP routes, but also in
// gateways, and for SNI. In those locations, we sort hostnames longest to shortest with wildcards last.
type Hostnames []Hostname

// prove we implement the interface at compile time
var _ sort.Interface = Hostnames{}

func (h Hostnames) Len() int {
	return len(h)
}

func (h Hostnames) Less(i, j int) bool {
	a, b := h[i], h[j]
	if len(a) == 0 && len(b) == 0 {
		return true // doesn't matter, they're both the empty string
	}

	// we sort longest to shortest, alphabetically, with wildcards last
	ai, aj := string(a[0]) == "*", string(b[0]) == "*"
	if ai && !aj {
		// h[i] is a wildcard, but h[j] isn't; therefore h[j] < h[i]
		return false
	} else if !ai && aj {
		// h[j] is a wildcard, but h[i] isn't; therefore h[i] < h[j]
		return true
	}

	// they're either both wildcards, or both not; in either case we sort them longest to shortest, alphabetically
	if len(a) == len(b) {
		return a < b
	}

	return len(a) > len(b)
}

func (h Hostnames) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h Hostnames) Contains(host Hostname) bool {
	for _, hHost := range h {
		if hHost == host {
			return true
		}
	}
	return false
}

// Intersection returns the subset of host names that are covered by both h and other.
// e.g.:
//  Hostnames(["foo.com","bar.com"]).Intersection(Hostnames(["*.com"]))         = Hostnames(["foo.com","bar.com"])
//  Hostnames(["foo.com","*.net"]).Intersection(Hostnames(["*.com","bar.net"])) = Hostnames(["foo.com","bar.net"])
//  Hostnames(["foo.com","*.net"]).Intersection(Hostnames(["*.bar.net"]))       = Hostnames(["*.bar.net"])
//  Hostnames(["foo.com"]).Intersection(Hostnames(["bar.com"]))                 = Hostnames([])
//  Hostnames([]).Intersection(Hostnames(["bar.com"])                           = Hostnames([])
func (h Hostnames) Intersection(other Hostnames) Hostnames {
	result := make(Hostnames, 0, len(h))
	for _, hHost := range h {
		for _, oHost := range other {
			if hHost.SubsetOf(oHost) {
				if !result.Contains(hHost) {
					result = append(result, hHost)
				}
			} else if oHost.SubsetOf(hHost) {
				if !result.Contains(oHost) {
					result = append(result, oHost)
				}
			}
		}
	}
	return result
}

// StringsToHostnames converts a slice of host name strings to type Hostnames.
func StringsToHostnames(hosts []string) Hostnames {
	result := make(Hostnames, 0, len(hosts))
	for _, host := range hosts {
		result = append(result, Hostname(host))
	}
	return result
}

// HostnamesForNamespace returns the subset of hosts that are in the specified namespace.
// The list of hosts contains host names optionally qualified with namespace/ or */.
// If not qualified or qualified with *, the host name is considered to be in every namespace.
// e.g.:
// HostnamesForNamespace(["ns1/foo.com","ns2/bar.com"], "ns1")   = Hostnames(["foo.com"])
// HostnamesForNamespace(["ns1/foo.com","ns2/bar.com"], "ns3")   = Hostnames([])
// HostnamesForNamespace(["ns1/foo.com","*/bar.com"], "ns1")     = Hostnames(["foo.com","bar.com"])
// HostnamesForNamespace(["ns1/foo.com","*/bar.com"], "ns3")     = Hostnames(["bar.com"])
// HostnamesForNamespace(["foo.com","ns2/bar.com"], "ns2")       = Hostnames(["foo.com","bar.com"])
// HostnamesForNamespace(["foo.com","ns2/bar.com"], "ns3")       = Hostnames(["foo.com"])
func HostnamesForNamespace(hosts []string, namespace string) Hostnames {
	result := make(Hostnames, 0, len(hosts))
	for _, host := range hosts {
		if strings.Contains(host, "/") {
			parts := strings.Split(host, "/")
			if parts[0] != namespace && parts[0] != "*" {
				continue
			}
			//strip the namespace
			host = parts[1]
		}
		result = append(result, Hostname(host))
	}
	return result
}
