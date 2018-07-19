// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package inmem

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"
)

// indices contains a mapping of non-ground references to values to sets of bindings.
//
//  +------+------------------------------------+
//  | ref1 | val1 | bindings-1, bindings-2, ... |
//  |      +------+-----------------------------+
//  |      | val2 | bindings-m, bindings-m, ... |
//  |      +------+-----------------------------+
//  |      | .... | ...                         |
//  +------+------+-----------------------------+
//  | ref2 | .... | ...                         |
//  +------+------+-----------------------------+
//  | ...                                       |
//  +-------------------------------------------+
//
// The "value" is the data value stored at the location referred to by the ground
// reference obtained by plugging bindings into the non-ground reference that is the
// index key.
//
type indices struct {
	mu    sync.Mutex
	table map[int]*indicesNode
}

type indicesNode struct {
	key  ast.Ref
	val  *bindingIndex
	next *indicesNode
}

func newIndices() *indices {
	return &indices{
		table: map[int]*indicesNode{},
	}
}

func (ind *indices) Build(ctx context.Context, store storage.Store, txn storage.Transaction, ref ast.Ref) (*bindingIndex, error) {

	ind.mu.Lock()
	defer ind.mu.Unlock()

	if exist := ind.get(ref); exist != nil {
		return exist, nil
	}

	index := newBindingIndex()

	if err := iterStorage(ctx, store, txn, ref, ast.EmptyRef(), ast.NewValueMap(), index.Add); err != nil {
		return nil, err
	}

	hashCode := ref.Hash()
	head := ind.table[hashCode]
	entry := &indicesNode{
		key:  ref,
		val:  index,
		next: head,
	}

	ind.table[hashCode] = entry

	return index, nil
}

func (ind *indices) get(ref ast.Ref) *bindingIndex {
	node := ind.getNode(ref)
	if node != nil {
		return node.val
	}
	return nil
}

func (ind *indices) iter(iter func(ast.Ref, *bindingIndex) error) error {
	for _, head := range ind.table {
		for entry := head; entry != nil; entry = entry.next {
			if err := iter(entry.key, entry.val); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ind *indices) getNode(ref ast.Ref) *indicesNode {
	hashCode := ref.Hash()
	for entry := ind.table[hashCode]; entry != nil; entry = entry.next {
		if entry.key.Equal(ref) {
			return entry
		}
	}
	return nil
}

func (ind *indices) String() string {
	buf := []string{}
	for _, head := range ind.table {
		for entry := head; entry != nil; entry = entry.next {
			str := fmt.Sprintf("%v: %v", entry.key, entry.val)
			buf = append(buf, str)
		}
	}
	return "{" + strings.Join(buf, ", ") + "}"
}

const (
	triggerID = "org.openpolicyagent/index-maintenance"
)

// bindingIndex contains a mapping of values to bindings.
type bindingIndex struct {
	table map[int]*indexNode
}

type indexNode struct {
	key  interface{}
	val  *bindingSet
	next *indexNode
}

func newBindingIndex() *bindingIndex {
	return &bindingIndex{
		table: map[int]*indexNode{},
	}
}

func (ind *bindingIndex) Add(val interface{}, bindings *ast.ValueMap) {

	node := ind.getNode(val)
	if node != nil {
		node.val.Add(bindings)
		return
	}

	hashCode := hash(val)
	bindingsSet := newBindingSet()
	bindingsSet.Add(bindings)

	entry := &indexNode{
		key:  val,
		val:  bindingsSet,
		next: ind.table[hashCode],
	}

	ind.table[hashCode] = entry
}

func (ind *bindingIndex) Lookup(_ context.Context, _ storage.Transaction, val interface{}, iter storage.IndexIterator) error {
	node := ind.getNode(val)
	if node == nil {
		return nil
	}
	return node.val.Iter(iter)
}

func (ind *bindingIndex) getNode(val interface{}) *indexNode {
	hashCode := hash(val)
	head := ind.table[hashCode]
	for entry := head; entry != nil; entry = entry.next {
		if util.Compare(entry.key, val) == 0 {
			return entry
		}
	}
	return nil
}

func (ind *bindingIndex) String() string {

	buf := []string{}

	for _, head := range ind.table {
		for entry := head; entry != nil; entry = entry.next {
			str := fmt.Sprintf("%v: %v", entry.key, entry.val)
			buf = append(buf, str)
		}
	}

	return "{" + strings.Join(buf, ", ") + "}"
}

type bindingSetNode struct {
	val  *ast.ValueMap
	next *bindingSetNode
}

type bindingSet struct {
	table map[int]*bindingSetNode
}

func newBindingSet() *bindingSet {
	return &bindingSet{
		table: map[int]*bindingSetNode{},
	}
}

func (set *bindingSet) Add(val *ast.ValueMap) {
	node := set.getNode(val)
	if node != nil {
		return
	}
	hashCode := val.Hash()
	head := set.table[hashCode]
	set.table[hashCode] = &bindingSetNode{val, head}
}

func (set *bindingSet) Iter(iter func(*ast.ValueMap) error) error {
	for _, head := range set.table {
		for entry := head; entry != nil; entry = entry.next {
			if err := iter(entry.val); err != nil {
				return err
			}
		}
	}
	return nil
}

func (set *bindingSet) String() string {
	buf := []string{}
	set.Iter(func(bindings *ast.ValueMap) error {
		buf = append(buf, bindings.String())
		return nil
	})
	return "{" + strings.Join(buf, ", ") + "}"
}

func (set *bindingSet) getNode(val *ast.ValueMap) *bindingSetNode {
	hashCode := val.Hash()
	for entry := set.table[hashCode]; entry != nil; entry = entry.next {
		if entry.val.Equal(val) {
			return entry
		}
	}
	return nil
}

func hash(v interface{}) int {
	switch v := v.(type) {
	case []interface{}:
		var h int
		for _, e := range v {
			h += hash(e)
		}
		return h
	case map[string]interface{}:
		var h int
		for k, v := range v {
			h += hash(k) + hash(v)
		}
		return h
	case string:
		h := fnv.New64a()
		h.Write([]byte(v))
		return int(h.Sum64())
	case bool:
		if v {
			return 1
		}
		return 0
	case nil:
		return 0
	case json.Number:
		h := fnv.New64a()
		h.Write([]byte(v))
		return int(h.Sum64())
	}
	panic(fmt.Sprintf("illegal argument: %v (%T)", v, v))
}

func iterStorage(ctx context.Context, store storage.Store, txn storage.Transaction, nonGround, ground ast.Ref, bindings *ast.ValueMap, iter func(interface{}, *ast.ValueMap)) error {

	if len(nonGround) == 0 {
		path, err := storage.NewPathForRef(ground)
		if err != nil {
			return err
		}
		node, err := store.Read(ctx, txn, path)
		if err != nil {
			if storage.IsNotFound(err) {
				return nil
			}
			return err
		}
		iter(node, bindings)
		return nil
	}

	head := nonGround[0]
	tail := nonGround[1:]

	headVar, isVar := head.Value.(ast.Var)

	if !isVar || len(ground) == 0 {
		ground = append(ground, head)
		return iterStorage(ctx, store, txn, tail, ground, bindings, iter)
	}

	path, err := storage.NewPathForRef(ground)
	if err != nil {
		return err
	}

	node, err := store.Read(ctx, txn, path)
	if err != nil {
		if storage.IsNotFound(err) {
			return nil
		}
		return err
	}

	switch node := node.(type) {
	case map[string]interface{}:
		for key := range node {
			ground = append(ground, ast.StringTerm(key))
			cpy := bindings.Copy()
			cpy.Put(headVar, ast.String(key))
			err := iterStorage(ctx, store, txn, tail, ground, cpy, iter)
			if err != nil {
				return err
			}
			ground = ground[:len(ground)-1]
		}
	case []interface{}:
		for i := range node {
			idx := ast.IntNumberTerm(i)
			ground = append(ground, idx)
			cpy := bindings.Copy()
			cpy.Put(headVar, idx.Value)
			err := iterStorage(ctx, store, txn, tail, ground, cpy, iter)
			if err != nil {
				return err
			}
			ground = ground[:len(ground)-1]
		}
	}

	return nil
}
