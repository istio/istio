// Copyright 2019 Istio Authors
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

package ledger

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"istio.io/istio/pkg/cache"
)

// The smt is derived from https://github.com/aergoio/SMT with modifications
// to remove unneeded features, and to support retention of old nodes for a fixed time.
// The aergoio smt license is as follows:
/*
MIT License

Copyright (c) 2018 aergo

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
© 2019 GitHub, Inc.
*/

// TODO when using the smt, make sure keys and values are same length as hash

// smt is a sparse Merkle tree.
type smt struct {
	rootMu sync.RWMutex
	// root is the current root of the smt.
	root []byte
	// defaultHashes are the default values of empty trees
	defaultHashes [][]byte
	// db holds the cache and related locks
	db *cacheDB
	// hash is the hash function used in the trie
	hash func(data ...[]byte) []byte
	// trieHeight is the number if bits in a key
	trieHeight int
	// the minimum length of time old nodes will be retained.
	retentionDuration time.Duration
	// lock is for the whole struct
	lock sync.RWMutex
	// atomicUpdate, commit all the changes made by intermediate update calls
	atomicUpdate bool
}

// this is the closest time.Duration comes to Forever, with a duration of ~145 years
// we can'tree use int64 max because the duration gets added to Now(), and the ints
// rollover, causing an immediate expiration (ironic, eh?)
const forever time.Duration = 1<<(63-1) - 1

// newSMT creates a new smt given a keySize, hash function, cache (nil will be defaulted to TTLCache), and retention
// duration for old nodes.
func newSMT(hash func(data ...[]byte) []byte, updateCache cache.ExpiringCache, retentionDuration time.Duration) *smt {
	if updateCache == nil {
		updateCache = cache.NewTTL(forever, time.Second)
	}
	s := &smt{
		hash:              hash,
		trieHeight:        len(hash([]byte("height"))) * 8, // hash any string to get output length
		retentionDuration: retentionDuration,
	}
	s.db = &cacheDB{
		updatedNodes: byteCache{cache: updateCache},
	}
	s.loadDefaultHashes()
	return s
}

func (s *smt) Root() []byte {
	s.rootMu.RLock()
	defer s.rootMu.RUnlock()
	return s.root
}

// loadDefaultHashes creates the default hashes
func (s *smt) loadDefaultHashes() {
	s.defaultHashes = make([][]byte, s.trieHeight+1)
	s.defaultHashes[0] = defaultLeaf
	var h []byte
	for i := 1; i <= s.trieHeight; i++ {
		h = s.hash(s.defaultHashes[i-1], s.defaultHashes[i-1])
		s.defaultHashes[i] = h
	}
}

// Update adds a sorted list of keys and their values to the trie
// If Update is called multiple times, only the state after the last update
// is committed.
// When calling Update multiple times without commit, make sure the
// values of different keys are unique(hash contains the key for example)
// otherwise some subtree may get overwritten with the wrong hash.
func (s *smt) Update(keys, values [][]byte) ([]byte, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.atomicUpdate = true
	ch := make(chan result, 1)
	s.update(s.Root(), keys, values, nil, 0, s.trieHeight, false, true, ch)
	result := <-ch
	if result.err != nil {
		return nil, result.err
	}
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	if len(result.update) != 0 {
		s.root = result.update[:hashLength]
	} else {
		s.root = nil
	}

	return s.root, nil
}

// result is used to contain the result of goroutines and is sent through a channel.
type result struct {
	update []byte
	err    error
}

// update adds a sorted list of keys and their values to the trie.
// It returns the root of the updated tree.
func (s *smt) update(root []byte, keys, values, batch [][]byte, iBatch, height int, shortcut, store bool, ch chan<- result) {
	if height == 0 {
		if bytes.Equal(values[0], defaultLeaf) {
			ch <- result{nil, nil}
		} else {
			ch <- result{values[0], nil}
		}
		return
	}
	batch, iBatch, lnode, rnode, isShortcut, err := s.loadChildren(root, height, iBatch, batch)
	if err != nil {
		ch <- result{nil, err}
		return
	}
	if isShortcut {
		keys, values = s.maybeAddShortcutToKV(keys, values, lnode[:hashLength], rnode[:hashLength])
		// The shortcut node was added to keys and values so consider this subtree default.
		lnode, rnode = nil, nil
		// update in the batch (set key, value to default to the next loadChildren is correct)
		batch[2*iBatch+1] = nil
		batch[2*iBatch+2] = nil
	}

	// Split the keys array so each branch can be updated in parallel
	// Does this require that keys are sorted?  Yes, see Update()
	lkeys, rkeys := s.splitKeys(keys, s.trieHeight-height)
	splitIndex := len(lkeys)
	lvalues, rvalues := values[:splitIndex], values[splitIndex:]

	if shortcut {
		store = false    // stop storing only after the shortcut node.
		shortcut = false // remove shortcut node flag
	}
	if len(lnode) == 0 && len(rnode) == 0 && len(keys) == 1 && store {
		if !bytes.Equal(values[0], defaultLeaf) {
			shortcut = true
		} else {
			// if the subtree contains only one key, store the key/value in a shortcut node
			store = false
		}
	}
	switch {
	case len(lkeys) == 0 && len(rkeys) > 0:
		s.updateRight(lnode, rnode, root, keys, values, batch, iBatch, height, shortcut, store, ch)
	case len(lkeys) > 0 && len(rkeys) == 0:
		s.updateLeft(lnode, rnode, root, keys, values, batch, iBatch, height, shortcut, store, ch)
	default:
		s.updateParallel(lnode, rnode, root, keys, values, batch, lkeys, rkeys, lvalues, rvalues, iBatch, height,
			shortcut, store, ch)
	}
}

// updateParallel updates both sides of the trie simultaneously
func (s *smt) updateParallel(lnode, rnode, root []byte, keys, values, batch, lkeys, rkeys, lvalues, rvalues [][]byte,
	iBatch, height int, shortcut, store bool, ch chan<- result,
) {
	// keys are separated between the left and right branches
	// update the branches in parallel
	lch := make(chan result, 1)
	rch := make(chan result, 1)
	go s.update(lnode, lkeys, lvalues, batch, 2*iBatch+1, height-1, shortcut, store, lch)
	go s.update(rnode, rkeys, rvalues, batch, 2*iBatch+2, height-1, shortcut, store, rch)
	lresult := <-lch
	rresult := <-rch
	if lresult.err != nil {
		ch <- result{nil, lresult.err}
		return
	}
	if rresult.err != nil {
		ch <- result{nil, rresult.err}
		return
	}
	ch <- result{s.interiorHash(lresult.update, rresult.update, height, iBatch, root, shortcut, store, keys,
		values, batch), nil}
}

// updateRight updates the right side of the tree
func (s *smt) updateRight(lnode, rnode, root []byte, keys, values, batch [][]byte, iBatch, height int, shortcut,
	store bool, ch chan<- result,
) {
	// all the keys go in the right subtree
	newch := make(chan result, 1)
	s.update(rnode, keys, values, batch, 2*iBatch+2, height-1, shortcut, store, newch)
	res := <-newch
	if res.err != nil {
		ch <- result{nil, res.err}
		return
	}
	ch <- result{s.interiorHash(lnode, res.update, height, iBatch, root, shortcut, store, keys, values,
		batch), nil}
}

// updateLeft updates the left side of the tree
func (s *smt) updateLeft(lnode, rnode, root []byte, keys, values, batch [][]byte, iBatch, height int, shortcut,
	store bool, ch chan<- result,
) {
	// all the keys go in the left subtree
	newch := make(chan result, 1)
	s.update(lnode, keys, values, batch, 2*iBatch+1, height-1, shortcut, store, newch)
	res := <-newch
	if res.err != nil {
		ch <- result{nil, res.err}
		return
	}
	ch <- result{s.interiorHash(res.update, rnode, height, iBatch, root, shortcut, store, keys, values,
		batch), nil}
}

// splitKeys divides the array of keys into 2 so they can update left and right branches in parallel
func (s *smt) splitKeys(keys [][]byte, height int) ([][]byte, [][]byte) {
	for i, key := range keys {
		if bitIsSet(key, height) {
			return keys[:i], keys[i:]
		}
	}
	return keys, nil
}

// maybeAddShortcutToKV adds a shortcut key to the keys array to be updated.
// this is used when a subtree containing a shortcut node is being updated
func (s *smt) maybeAddShortcutToKV(keys, values [][]byte, shortcutKey, shortcutVal []byte) ([][]byte, [][]byte) {
	newKeys := make([][]byte, 0, len(keys)+1)
	newVals := make([][]byte, 0, len(keys)+1)

	if bytes.Compare(shortcutKey, keys[0]) < 0 {
		newKeys = append(newKeys, shortcutKey)
		newKeys = append(newKeys, keys...)
		newVals = append(newVals, shortcutVal)
		newVals = append(newVals, values...)
	} else if bytes.Compare(shortcutKey, keys[len(keys)-1]) > 0 {
		newKeys = append(newKeys, keys...)
		newKeys = append(newKeys, shortcutKey)
		newVals = append(newVals, values...)
		newVals = append(newVals, shortcutVal)
	} else {
		higher := false
		for i, key := range keys {
			if bytes.Equal(shortcutKey, key) {
				// the shortcut keys is being updated
				return keys, values
			}
			if !higher && bytes.Compare(shortcutKey, key) > 0 {
				higher = true
				continue
			}
			if higher && bytes.Compare(shortcutKey, key) < 0 {
				// insert shortcut in slices
				newKeys = append(newKeys, keys[:i]...)
				newKeys = append(newKeys, shortcutKey)
				newKeys = append(newKeys, keys[i:]...)
				newVals = append(newVals, values[:i]...)
				newVals = append(newVals, shortcutVal)
				newVals = append(newVals, values[i:]...)
				break
			}
		}
	}
	return newKeys, newVals
}

const batchLen int = 31

// loadChildren looks for the children of a node.
// if the node is not stored in cache, it will be loaded from db.
func (s *smt) loadChildren(root []byte, height, iBatch int, batch [][]byte) ([][]byte, int, []byte, []byte, bool,
	error,
) {
	isShortcut := false
	if height%4 == 0 {
		if len(root) == 0 {
			// create a new default batch
			batch = make([][]byte, batchLen)
			batch[0] = []byte{0}
		} else {
			var err error
			batch, err = s.loadBatch(root[:hashLength])
			if err != nil {
				return nil, 0, nil, nil, false, err
			}
		}
		iBatch = 0
		if batch[0][0] == 1 {
			isShortcut = true
		}
	} else if len(batch[iBatch]) != 0 && batch[iBatch][hashLength] == 1 {
		isShortcut = true
	}
	return batch, iBatch, batch[2*iBatch+1], batch[2*iBatch+2], isShortcut, nil
}

// loadBatch fetches a batch of nodes in cache or db
func (s *smt) loadBatch(root []byte) ([][]byte, error) {
	var node hash
	copy(node[:], root)

	// checking updated nodes is useful if get() or update() is called twice in a row without db commit
	s.db.updatedMux.RLock()
	val, exists := s.db.updatedNodes.Get(node)
	s.db.updatedMux.RUnlock()
	if exists {
		if s.atomicUpdate {
			// Return a copy so that Commit() doesn't have to be called at
			// each block and still commit every state transition.
			newVal := make([][]byte, batchLen)
			copy(newVal, val)
			return newVal, nil
		}
		return val, nil
	}
	return nil, fmt.Errorf("the trie node %x is unavailable in the disk db, db may be corrupted", root)
}

// interiorHash hashes 2 children to get the parent hash and stores it in the updatedNodes and maybe in liveCache.
// the key is the hash and the value is the appended child nodes or the appended key/value in case of a shortcut.
// keys of go mappings cannot be byte slices so the hash is copied to a byte array
func (s *smt) interiorHash(left, right []byte, height, iBatch int, oldRoot []byte, shortcut, store bool, keys, values,
	batch [][]byte,
) []byte {
	var h []byte
	if len(left) == 0 && len(right) == 0 {
		// if a key was deleted, the node becomes default
		batch[2*iBatch+1] = left
		batch[2*iBatch+2] = right
		s.deleteOldNode(oldRoot)
		return nil
	} else if len(left) == 0 {
		h = s.hash(s.defaultHashes[height-1], right[:hashLength])
	} else if len(right) == 0 {
		h = s.hash(left[:hashLength], s.defaultHashes[height-1])
	} else {
		h = s.hash(left[:hashLength], right[:hashLength])
	}
	if !store {
		// a shortcut node cannot move up
		return append(h, 0)
	}
	if !shortcut {
		h = append(h, 0)
	} else {
		// store the value at the shortcut node instead of height 0.
		h = append(h, 1)
		left = append(keys[0], 2)
		right = append(values[0], 2)
	}
	batch[2*iBatch+2] = right
	batch[2*iBatch+1] = left

	// maybe store batch node
	if (height)%4 == 0 {
		if shortcut {
			batch[0] = []byte{1}
		} else {
			batch[0] = []byte{0}
		}

		s.storeNode(batch, h, oldRoot)
	}
	return h
}

// storeNode stores a batch and deletes the old node from cache
func (s *smt) storeNode(batch [][]byte, h, oldRoot []byte) {
	if !bytes.Equal(h, oldRoot) {
		var node hash
		copy(node[:], h)
		// record new node
		s.db.updatedMux.Lock()
		s.db.updatedNodes.Set(node, batch)
		s.db.updatedMux.Unlock()
		s.deleteOldNode(oldRoot)
	}
}

// deleteOldNode deletes an old node that has been updated
func (s *smt) deleteOldNode(root []byte) {
	var node hash
	copy(node[:], root)
	if !s.atomicUpdate {
		// dont delete old nodes with atomic updated except when
		// moving up a shortcut, we dont record every single move
		s.db.updatedMux.Lock()
		// mark for expiration?
		if val, ok := s.db.updatedNodes.Get(node); ok {
			s.db.updatedNodes.SetWithExpiration(node, val, s.retentionDuration)
		}
		s.db.updatedMux.Unlock()
	}
}
