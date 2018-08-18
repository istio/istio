// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"github.com/open-policy-agent/opa/ast"
)

type virtualCache struct {
	stack []*virtualCacheElem
}

type virtualCacheElem struct {
	value    *ast.Term
	children map[ast.Value]*virtualCacheElem
}

func newVirtualCache() *virtualCache {
	cache := &virtualCache{}
	cache.Push()
	return cache
}

func (c *virtualCache) Push() {
	c.stack = append(c.stack, newVirtualCacheElem())
}

func (c *virtualCache) Pop() {
	c.stack = c.stack[:len(c.stack)-1]
}

func (c *virtualCache) Get(ref ast.Ref) *ast.Term {
	node := c.stack[len(c.stack)-1]
	for i := 0; i < len(ref); i++ {
		key := ref[i].Value
		next := node.children[key]
		if next == nil {
			return nil
		}
		node = next
	}
	return node.value
}

func (c *virtualCache) Put(ref ast.Ref, value *ast.Term) {
	node := c.stack[len(c.stack)-1]
	for i := 0; i < len(ref); i++ {
		key := ref[i].Value
		next := node.children[key]
		if next == nil {
			next = newVirtualCacheElem()
			node.children[key] = next
		}
		node = next
	}
	node.value = value
}

func newVirtualCacheElem() *virtualCacheElem {
	return &virtualCacheElem{
		children: map[ast.Value]*virtualCacheElem{},
	}
}

// baseCache implements a trie structure to cache base documents read out of
// storage. Values inserted into the cache may contain other values that were
// previously inserted. In this case, the previous values are erased from the
// structure.
type baseCache struct {
	root *baseCacheElem
}

func newBaseCache() *baseCache {
	return &baseCache{
		root: newBaseCacheElem(),
	}
}

func (c *baseCache) Get(ref ast.Ref) ast.Value {
	node := c.root
	for i := 0; i < len(ref); i++ {
		node = node.children[ref[i].Value]
		if node == nil {
			return nil
		} else if node.value != nil {
			result, err := node.value.Find(ref[i+1:])
			if err != nil {
				return nil
			}
			return result
		}
	}
	return nil
}

func (c *baseCache) Put(ref ast.Ref, value ast.Value) {
	node := c.root
	for i := 0; i < len(ref); i++ {
		if child, ok := node.children[ref[i].Value]; ok {
			node = child
		} else {
			child := newBaseCacheElem()
			node.children[ref[i].Value] = child
			node = child
		}
	}
	node.set(value)
}

type baseCacheElem struct {
	value    ast.Value
	children map[ast.Value]*baseCacheElem
}

func newBaseCacheElem() *baseCacheElem {
	return &baseCacheElem{
		children: map[ast.Value]*baseCacheElem{},
	}
}

func (e *baseCacheElem) set(value ast.Value) {
	e.value = value
	e.children = map[ast.Value]*baseCacheElem{}
}
