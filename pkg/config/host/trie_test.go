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

	"github.com/stretchr/testify/assert"
	"istio.io/istio/pkg/slices"
)

func TestAdd(t *testing.T) {
	cases := []struct {
		name     string
		hosts    []string
		wantTrie *Trie[string]
	}{
		{
			name:  "empty host",
			hosts: []string{},
			wantTrie: &Trie[string]{
				data:     nil,
				children: map[string]*Trie[string]{},
			},
		},
		{
			name:  "one exact host",
			hosts: []string{"b.a"},
			wantTrie: &Trie[string]{
				children: map[string]*Trie[string]{
					"a": {children: map[string]*Trie[string]{
						"b": {
							data:     []string{"b.a"},
							children: map[string]*Trie[string]{},
						},
					}},
				},
			},
		},
		{
			name:  "duplicate host",
			hosts: []string{"b.a", "b.a"},
			wantTrie: &Trie[string]{
				children: map[string]*Trie[string]{
					"a": {children: map[string]*Trie[string]{
						"b": {
							data:     []string{"b.a", "b.a"},
							children: map[string]*Trie[string]{},
						},
					}},
				},
			},
		},
		{
			name:  "multi exact host",
			hosts: []string{"b.a", "c.b.a", "c.b"},
			wantTrie: &Trie[string]{
				children: map[string]*Trie[string]{
					"a": {children: map[string]*Trie[string]{
						"b": {
							data: []string{"b.a"},
							children: map[string]*Trie[string]{
								"c": {
									data:     []string{"c.b.a"},
									children: map[string]*Trie[string]{},
								},
							},
						},
					}},
					"b": {children: map[string]*Trie[string]{
						"c": {
							data:     []string{"c.b"},
							children: map[string]*Trie[string]{},
						},
					}},
				},
			},
		},
		{
			name:  "with wildcard host",
			hosts: []string{"*.a", "b.a", "c.b.a", "*.b.a"},
			wantTrie: &Trie[string]{
				children: map[string]*Trie[string]{
					"a": {children: map[string]*Trie[string]{
						"*": {
							data:     []string{"*.a"},
							children: map[string]*Trie[string]{},
						},
						"b": {
							data: []string{"b.a"},
							children: map[string]*Trie[string]{
								"c": {
									data:     []string{"c.b.a"},
									children: map[string]*Trie[string]{},
								},
								"*": {
									data:     []string{"*.b.a"},
									children: map[string]*Trie[string]{},
								},
							},
						},
					}},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := NewTrie[string]()
			for _, h := range c.hosts {
				tr.Add(h, h)
			}
			assert.Equal(t, tr, c.wantTrie)
		})
	}
}

func TestMatches(t *testing.T) {
	cases := []struct {
		egressHost   string
		wantMatches  []string
		wantSubsetOf []string
	}{
		{
			egressHost:   "",
			wantMatches:  []string{},
			wantSubsetOf: []string{},
		},
		{
			egressHost:   "*",
			wantMatches:  []string{"*.a", "b.a", "c.b.a", "d.b.a", "*.c.b.a", "e.d.b.a"},
			wantSubsetOf: []string{"*.a", "b.a", "c.b.a", "d.b.a", "*.c.b.a", "e.d.b.a"},
		},
		{
			egressHost:   "*.a",
			wantMatches:  []string{"*.a", "b.a", "c.b.a", "d.b.a", "*.c.b.a", "e.d.b.a"},
			wantSubsetOf: []string{"*.a", "b.a", "c.b.a", "d.b.a", "*.c.b.a", "e.d.b.a"},
		},
		{
			egressHost:   "b.a",
			wantMatches:  []string{"*.a", "b.a"},
			wantSubsetOf: []string{"b.a"},
		},
		{
			egressHost:   "c.a",
			wantMatches:  []string{"*.a"},
			wantSubsetOf: []string{},
		},
		{
			egressHost:   "*.b.a",
			wantMatches:  []string{"*.a", "c.b.a", "d.b.a", "*.c.b.a", "e.d.b.a"},
			wantSubsetOf: []string{"c.b.a", "d.b.a", "*.c.b.a", "e.d.b.a"},
		},
		{
			egressHost:   "c.b.a",
			wantMatches:  []string{"*.a", "c.b.a"},
			wantSubsetOf: []string{"c.b.a"},
		},
		{
			egressHost:   "c.xx.a",
			wantMatches:  []string{"*.a"},
			wantSubsetOf: []string{},
		},
		{
			egressHost:   "*.d.b.a",
			wantMatches:  []string{"*.a", "e.d.b.a"},
			wantSubsetOf: []string{"e.d.b.a"},
		},
		{
			egressHost:   "*.c.b.a",
			wantMatches:  []string{"*.a", "*.c.b.a"},
			wantSubsetOf: []string{"*.c.b.a"},
		},
		{
			egressHost:   "foo.bar",
			wantMatches:  []string{},
			wantSubsetOf: []string{},
		},
	}

	//    a
	//   / \
	//  *   b
	//     / \
	//     c  d
	//     |  |
	//     *  e
	// build trie tree
	vsHosts := []string{"*.a", "b.a", "c.b.a", "d.b.a", "*.c.b.a", "e.d.b.a"}
	tr := NewTrie[string]()
	for _, h := range vsHosts {
		tr.Add(h, h)
	}

	for _, c := range cases {
		t.Run(c.egressHost, func(t *testing.T) {
			gh := strings.Split(c.egressHost, ".")

			// test Matches
			g1 := make([]string, 0)
			g1 = tr.Matches(gh, g1)
			assert.Equal(t, slices.Sort(g1), slices.Sort(c.wantMatches))

			// test SubsetOf
			g2 := make([]string, 0)
			g2 = tr.SubsetOf(gh, g2)
			assert.Equal(t, slices.Sort(g2), slices.Sort(c.wantSubsetOf))
		})
	}
}

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
			tr.Add(vh, vh)
		}

		// test matches
		for _, eh := range egressHosts {
			a2 := strings.Split(eh, ".")
			// call can guess the size
			out := make([]string, 10)
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
			tr.Add(vh, vh)
		}

		// test subsetOf matches
		for _, eh := range egressHosts {
			a2 := strings.Split(eh, ".")
			// call can guess the size
			out := make([]string, 10)
			for i := 0; i < b.N; i++ {
				out = out[:0]
				tr.SubsetOf(a2, out)
			}
		}
	})
}
