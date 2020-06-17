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

package host

import (
	"strings"
)

// Name describes a (possibly wildcarded) hostname
type Name string

// Matches returns true if this hostname overlaps with the other hostname. Names overlap if:
// - they're fully resolved (i.e. not wildcarded) and match exactly (i.e. an exact string match)
// - one or both are wildcarded (e.g. "*.foo.com"), in which case we use wildcard resolution rules
// to determine if h is covered by o or o is covered by h.
// e.g.:
//  Name("foo.com").Matches("foo.com")   = true
//  Name("foo.com").Matches("bar.com")   = false
//  Name("*.com").Matches("foo.com")     = true
//  Name("bar.com").Matches("*.com")     = true
//  Name("*.foo.com").Matches("foo.com") = false
//  Name("*").Matches("foo.com")         = true
//  Name("*").Matches("*.com")           = true
func (n Name) Matches(o Name) bool {
	hWildcard := n.IsWildCarded()
	oWildcard := o.IsWildCarded()

	if hWildcard {
		if oWildcard {
			// both n and o are wildcards
			if len(n) < len(o) {
				return strings.HasSuffix(string(o[1:]), string(n[1:]))
			}
			return strings.HasSuffix(string(n[1:]), string(o[1:]))
		}
		// only n is wildcard
		return strings.HasSuffix(string(o), string(n[1:]))
	}

	if oWildcard {
		// only o is wildcard
		return strings.HasSuffix(string(n), string(o[1:]))
	}

	// both are non-wildcards, so do normal string comparison
	return n == o
}

// SubsetOf returns true if this hostname is a valid subset of the other hostname. The semantics are
// the same as "Matches", but only in one direction (i.e., h is covered by o).
func (n Name) SubsetOf(o Name) bool {
	hWildcard := n.IsWildCarded()
	oWildcard := o.IsWildCarded()

	if hWildcard {
		if oWildcard {
			// both n and o are wildcards
			if len(n) < len(o) {
				return false
			}
			return strings.HasSuffix(string(n[1:]), string(o[1:]))
		}
		// only n is wildcard
		return false
	}

	if oWildcard {
		// only o is wildcard
		return strings.HasSuffix(string(n), string(o[1:]))
	}

	// both are non-wildcards, so do normal string comparison
	return n == o
}

func (n Name) IsWildCarded() bool {
	return len(n) > 0 && n[0] == '*'
}
