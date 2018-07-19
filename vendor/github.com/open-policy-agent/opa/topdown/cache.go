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
	c.stack = c.stack[:len(c.stack)]
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
