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

import "strings"

type trieChild[T any] map[string]*Trie[T]

type Trie[T any] struct {
	child trieChild[T]
	data  []T
}

func NewTrie[T any]() *Trie[T] {
	return &Trie[T]{
		child: make(trieChild[T]),
	}
}

func (t *Trie[T]) Add(host []string, data T) {
	if len(host) == 0 {
		t.data = append(t.data, data)
		return
	}

	// suffix tree
	key := host[len(host)-1]
	left := host[:len(host)-1]

	child, ok := t.child[key]
	if !ok {
		child = NewTrie[T]()
		t.child[key] = child
	}

	child.Add(left, data)
}

func (t *Trie[T]) AddBatch(hosts []string, data T) {
	for _, h := range hosts {
		t.Add(strings.Split(h, "."), data)
	}
}

func (t *Trie[T]) Matches(host []string, out []T) []T {
	if len(host) == 0 {
		if len(t.child) == 0 {
			return t.getData(out)
		}
		return out
	}

	if _, ok := t.child["*"]; ok {
		return t.getData(out)
	}

	key := host[len(host)-1]
	left := host[:len(host)-1]

	child, exists := t.child[key]
	if exists {
		return child.Matches(left, out)
	}

	if len(t.child) == 0 {
		return out
	}

	if key == "*" {
		return t.getData(out)
	}

	return out
}

func (t *Trie[T]) SubsetOf(host []string, out []T) []T {
	if len(host) == 0 {
		if len(t.child) == 0 {
			return t.getData(out)
		}
		return out
	}

	key := host[len(host)-1]
	left := host[:len(host)-1]

	child, exists := t.child[key]
	if exists {
		return child.SubsetOf(left, out)
	}

	if len(t.child) == 0 {
		return out
	}

	if key == "*" {
		return t.getData(out)
	}

	return out
}

func (t *Trie[T]) getData(out []T) []T {
	if t == nil {
		return out
	}
	if len(t.data) != 0 {
		out = append(out, t.data...)
	}
	if len(t.child) == 0 {
		return out
	}

	for _, child := range t.child {
		out = child.getData(out)
	}
	return out
}
