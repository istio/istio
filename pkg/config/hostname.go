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

// This file describes the abstract model of services (and their instances) as
// represented in Istio. This model is independent of the underlying platform
// (Kubernetes, Mesos, etc.). Platform specific adapters found populate the
// model object with various fields, from the metadata found in the platform.
// The platform independent proxy code uses the representation in the model to
// generate the configuration files for the Layer 7 proxy sidecar. The proxy
// code is specific to individual proxy implementations

package config

import (
	"strings"
)

// Hostname describes a (possibly wildcarded) hostname
type Hostname string

// Matches returns true if this hostname overlaps with the other hostname. Hostnames overlap if:
// - they're fully resolved (i.e. not wildcarded) and match exactly (i.e. an exact string match)
// - one or both are wildcarded (e.g. "*.foo.com"), in which case we use wildcard resolution rules
// to determine if h is covered by o or o is covered by h.
// e.g.:
//  Hostname("foo.com").Matches("foo.com")   = true
//  Hostname("foo.com").Matches("bar.com")   = false
//  Hostname("*.com").Matches("foo.com")     = true
//  Hostname("bar.com").Matches("*.com")     = true
//  Hostname("*.foo.com").Matches("foo.com") = false
//  Hostname("*").Matches("foo.com")         = true
//  Hostname("*").Matches("*.com")           = true
func (h Hostname) Matches(o Hostname) bool {
	hWildcard := len(h) > 0 && string(h[0]) == "*"
	oWildcard := len(o) > 0 && string(o[0]) == "*"

	if hWildcard {
		if oWildcard {
			// both h and o are wildcards
			if len(h) < len(o) {
				return strings.HasSuffix(string(o[1:]), string(h[1:]))
			}
			return strings.HasSuffix(string(h[1:]), string(o[1:]))
		}
		// only h is wildcard
		return strings.HasSuffix(string(o), string(h[1:]))
	}

	if oWildcard {
		// only o is wildcard
		return strings.HasSuffix(string(h), string(o[1:]))
	}

	// both are non-wildcards, so do normal string comparison
	return h == o
}

// SubsetOf returns true if this hostname is a valid subset of the other hostname. The semantics are
// the same as "Matches", but only in one direction (i.e., h is covered by o).
func (h Hostname) SubsetOf(o Hostname) bool {
	hWildcard := len(h) > 0 && string(h[0]) == "*"
	oWildcard := len(o) > 0 && string(o[0]) == "*"

	if hWildcard {
		if oWildcard {
			// both h and o are wildcards
			if len(h) < len(o) {
				return false
			}
			return strings.HasSuffix(string(h[1:]), string(o[1:]))
		}
		// only h is wildcard
		return false
	}

	if oWildcard {
		// only o is wildcard
		return strings.HasSuffix(string(h), string(o[1:]))
	}

	// both are non-wildcards, so do normal string comparison
	return h == o
}
