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

type Trie[T any] struct {
	data     []T
	children map[string]*Trie[T]
}

func NewTrie[T any]() *Trie[T] {
	return &Trie[T]{
		data:     nil,
		children: make(map[string]*Trie[T]),
	}
}

func (t *Trie[T]) Add(host string, data T) {
	root := t
	if host == "" {
		child := NewTrie[T]()
		root.children[""] = child
		root = child
		root.data = append(root.data, data)
		return
	}

	end := len(host)
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] != '.' {
			continue
		}
		key := host[i+1 : end]
		end = i

		child, exists := root.children[key]
		if !exists {
			child = NewTrie[T]()
			root.children[key] = child
		}
		root = child
	}

	if end >= 1 && host[0] != '.' {
		key := host[0:end]
		child, exists := root.children[key]
		if !exists {
			child = NewTrie[T]()
			root.children[key] = child
		}
		root = child
	}

	root.data = append(root.data, data)
}

func (t *Trie[T]) AddBatch(hosts []string, data T) {
	for _, host := range hosts {
		t.Add(host, data)
	}
}

// Matches
// the `host` param is host fragments, eg "foo.bar", the host param is ["foo", "bar"].
// the `out` param is used for caller to control the capacity.
func (t *Trie[T]) Matches(host []string, out []T) []T {
	root := t
	for i := len(host) - 1; i >= 0; i-- {
		key := host[i]

		// meet wildcard, all child nodes matches (excluding root itself)
		if key == "*" {
			return root.childrenData(out)
		}

		// sibling nodes at the same level have wildcard
		if wc, ok := root.children["*"]; ok {
			out = append(out, wc.data...)
		}

		child, ok := root.children[key]
		if !ok {
			return out
		}
		root = child
	}

	// it means that two hosts are found to be equal
	if root.data != nil {
		out = append(out, root.data...)
	}

	return out
}

func (t *Trie[T]) SubsetOf(host []string, out []T) []T {
	root := t
	for i := len(host) - 1; i >= 0; i-- {
		key := host[i]
		if key == "*" {
			return root.childrenData(out)
		}

		child, ok := root.children[key]
		if !ok {
			return out
		}
		root = child
	}

	if root.data != nil {
		out = append(out, root.data...)
	}

	return out
}

func (t *Trie[T]) childrenData(out []T) []T {
	for _, v := range t.children {
		out = v.getData(out)
	}
	return out
}

func (t *Trie[T]) getData(out []T) []T {
	if t.data != nil {
		out = append(out, t.data...)
	}

	// the leaf node
	if len(t.children) == 0 {
		return out
	}

	for _, c := range t.children {
		out = c.getData(out)
	}

	return out
}
