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
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func BenchmarkMatches(b *testing.B) {
	egressHosts := []string{
		"v1.productpage.cluster.local.svc",
		"v1.reviews.cluster.local.svc",
		"v2.reviews.cluster.local.svc",
		"v3.reviews.cluster.local.svc",
		"v1.details.cluster.local.svc",
		"v1.ratings.cluster.local.svc",
		"*.wildcard.com",
	}
	vsHosts := make([]string, 0)
	vsHosts = append(vsHosts, egressHosts...)
	for i := 0; i < 3; i++ {
		for _, h := range egressHosts {
			if strings.HasPrefix(h, "*") {
				vsHosts = append(vsHosts, fmt.Sprintf("*.%d%s", i, h[1:]))
				continue
			}
			vsHosts = append(vsHosts, fmt.Sprintf("%d.%s", i, h))
		}
	}
	egressHosts = append(egressHosts, "notexists.com", "*.notexists.com")

	// shuffle the slice
	rand.Shuffle(len(vsHosts), func(i, j int) {
		vsHosts[i], vsHosts[j] = vsHosts[j], vsHosts[i]
	})
	rand.Shuffle(len(egressHosts), func(i, j int) {
		egressHosts[i], egressHosts[j] = egressHosts[j], egressHosts[i]
	})

	b.ResetTimer()
	b.Run("old matches", func(b *testing.B) {
		for _, eh := range egressHosts {
			for i := 0; i < b.N; i++ {
				for _, vh := range vsHosts {
					Name(vh).Matches(Name(eh))
				}
			}
		}
	})

	b.Run("trie matches", func(b *testing.B) {
		// build trie
		tr := NewTrie[string]()
		for _, vh := range vsHosts {
			tr.Add(strings.Split(vh, "."), vh)
		}

		// test matches
		for _, eh := range egressHosts {
			a2 := strings.Split(eh, ".")
			out := make([]string, 0)
			for i := 0; i < b.N; i++ {
				out = out[:0]
				tr.Matches(a2, out)
			}
		}
	})

	b.Run("old subsetOf", func(b *testing.B) {
		for _, eh := range egressHosts {
			for i := 0; i < b.N; i++ {
				for _, vh := range vsHosts {
					Name(vh).SubsetOf(Name(eh))
				}
			}
		}
	})

	b.Run("trie subsetOf", func(b *testing.B) {
		// build trie
		tr := NewTrie[string]()
		for _, vh := range vsHosts {
			tr.Add(strings.Split(vh, "."), vh)
		}

		// test subsetOf matches
		for _, eh := range egressHosts {
			a2 := strings.Split(eh, ".")
			out := make([]string, 0)
			for i := 0; i < b.N; i++ {
				out = out[:0]
				tr.SubsetOf(a2, out)
			}
		}
	})
}
