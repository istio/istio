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
	"crypto/rand"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/cache"
	"istio.io/istio/pkg/test/util/assert"
)

func TestSmtEmptyTrie(t *testing.T) {
	smt := newSMT(hasher, nil, time.Minute)
	if !bytes.Equal([]byte{}, smt.root) {
		t.Fatal("empty trie root hash not correct")
	}
}

func TestSmtUpdateAndGet(t *testing.T) {
	smt := newSMT(hasher, nil, time.Minute)
	smt.atomicUpdate = false

	// Add data to empty trie
	keys := getFreshData(10)
	values := getFreshData(10)
	ch := make(chan result, 1)
	smt.update(smt.root, keys, values, nil, 0, smt.trieHeight, false, true, ch)
	res := <-ch
	root := res.update

	// Check all keys have been stored
	for i, key := range keys {
		value, _ := smt.get(root, key, nil, 0, smt.trieHeight)
		if !bytes.Equal(values[i], value) {
			t.Fatal("value not updated")
		}
	}

	// Append to the trie
	newKeys := getFreshData(5)
	newValues := getFreshData(5)
	ch = make(chan result, 1)
	smt.update(root, newKeys, newValues, nil, 0, smt.trieHeight, false, true, ch)
	res = <-ch
	newRoot := res.update
	if bytes.Equal(root, newRoot) {
		t.Fatal("trie not updated")
	}
	for i, newKey := range newKeys {
		newValue, _ := smt.get(newRoot, newKey, nil, 0, smt.trieHeight)
		if !bytes.Equal(newValues[i], newValue) {
			t.Fatal("failed to get value")
		}
	}
	// Check old keys are still stored
	for i, key := range keys {
		value, _ := smt.get(newRoot, key, nil, 0, smt.trieHeight)
		if !bytes.Equal(values[i], value) {
			t.Fatal("failed to get value")
		}
	}
}

func TestTrieAtomicUpdate(t *testing.T) {
	smt := newSMT(hasher, nil, time.Minute)
	keys := getFreshData(10)
	values := getFreshData(10)
	root, _ := smt.Update(keys, values)

	// check keys of previous atomic update are accessible in
	// updated nodes with root.
	smt.atomicUpdate = false
	for i, key := range keys {
		value, _ := smt.get(root, key, nil, 0, smt.trieHeight)
		if !bytes.Equal(values[i], value) {
			t.Fatal("failed to get value")
		}
	}
}

func TestSmtPublicUpdateAndGet(t *testing.T) {
	smt := newSMT(hasher, nil, time.Minute)
	// Add data to empty trie
	keys := getFreshData(5)
	values := getFreshData(5)
	root, _ := smt.Update(keys, values)

	// Check all keys have been stored
	for i, key := range keys {
		value, _ := smt.Get(key)
		if !bytes.Equal(values[i], value) {
			t.Fatal("trie not updated")
		}
	}
	if !bytes.Equal(root, smt.root) {
		t.Fatal("root not stored")
	}

	newValues := getFreshData(5)
	_, err := smt.Update(keys, newValues)
	assert.NoError(t, err)

	// Check all keys have been modified
	for i, key := range keys {
		value, _ := smt.Get(key)
		if !bytes.Equal(newValues[i], value) {
			t.Fatal("trie not updated")
		}
	}

	newKeys := getFreshData(5)
	newValues = getFreshData(5)
	_, err = smt.Update(newKeys, newValues)
	assert.NoError(t, err)
	for i, key := range newKeys {
		value, _ := smt.Get(key)
		if !bytes.Equal(newValues[i], value) {
			t.Fatal("trie not updated")
		}
	}
}

func TestSmtDelete(t *testing.T) {
	smt := newSMT(hasher, nil, time.Minute)
	// Add data to empty trie
	keys := getFreshData(10)
	values := getFreshData(10)
	ch := make(chan result, 1)
	smt.update(smt.root, keys, values, nil, 0, smt.trieHeight, false, true, ch)
	res := <-ch
	root := res.update
	value, _ := smt.get(root, keys[0], nil, 0, smt.trieHeight)
	if !bytes.Equal(values[0], value) {
		t.Fatal("trie not updated")
	}

	// Delete from trie
	// To delete a key, just set it's value to Default leaf hash.
	ch = make(chan result, 1)
	smt.update(root, keys[0:1], [][]byte{defaultLeaf}, nil, 0, smt.trieHeight, false, true, ch)
	res = <-ch
	newRoot := res.update
	newValue, _ := smt.get(newRoot, keys[0], nil, 0, smt.trieHeight)
	if len(newValue) != 0 {
		t.Fatal("Failed to delete from trie")
	}
	// Remove deleted key from keys and check root with a clean trie.
	smt2 := newSMT(hasher, nil, time.Minute)
	ch = make(chan result, 1)
	smt2.update(smt2.root, keys[1:], values[1:], nil, 0, smt.trieHeight, false, true, ch)
	res = <-ch
	cleanRoot := res.update
	if !bytes.Equal(newRoot, cleanRoot) {
		t.Fatal("roots mismatch")
	}

	// Empty the trie
	var newValues [][]byte
	for i := 0; i < 10; i++ {
		newValues = append(newValues, defaultLeaf)
	}
	ch = make(chan result, 1)
	smt.update(root, keys, newValues, nil, 0, smt.trieHeight, false, true, ch)
	res = <-ch
	root = res.update
	if len(root) != 0 {
		t.Fatal("empty trie root hash not correct")
	}
	// Test deleting an already empty key
	smt = newSMT(hasher, nil, time.Minute)
	keys = getFreshData(2)
	values = getFreshData(2)
	root, _ = smt.Update(keys, values)
	key0 := make([]byte, 8)
	key1 := make([]byte, 8)
	_, err := smt.Update([][]byte{key0, key1}, [][]byte{defaultLeaf, defaultLeaf})
	assert.NoError(t, err)
	if !bytes.Equal(root, smt.root) {
		t.Fatal("deleting a default key shouldn't modify the tree")
	}
}

// test updating and deleting at the same time
func TestTrieUpdateAndDelete(t *testing.T) {
	smt := newSMT(hasher, nil, time.Minute)
	key0 := make([]byte, 8)
	values := getFreshData(1)
	root, _ := smt.Update([][]byte{key0}, values)
	smt.atomicUpdate = false
	_, _, k, v, isShortcut, _ := smt.loadChildren(root, smt.trieHeight, 0, nil)
	if !isShortcut || !bytes.Equal(k[:hashLength], key0) || !bytes.Equal(v[:hashLength], values[0]) {
		t.Fatal("leaf shortcut didn't move up to root")
	}

	key1 := make([]byte, 8)
	// set the last bit
	bitSet(key1, 63)
	keys := [][]byte{key0, key1}
	values = [][]byte{defaultLeaf, getFreshData(1)[0]}
	_, err := smt.Update(keys, values)
	assert.NoError(t, err)
}

func bitSet(bits []byte, i int) {
	bits[i/8] |= 1 << uint(7-i%8)
}

func TestSmtRaisesError(t *testing.T) {
	smt := newSMT(hasher, nil, time.Minute)
	// Add data to empty trie
	keys := getFreshData(10)
	values := getFreshData(10)
	_, err := smt.Update(keys, values)
	assert.NoError(t, err)
	smt.db.updatedNodes = byteCache{cache: cache.NewTTL(forever, time.Minute)}
	smt.loadDefaultHashes()

	// Check errors are raised is a keys is not in cache nor db
	for _, key := range keys {
		_, err := smt.Get(key)
		assert.Error(t, err)
		assert.Equal(t, strings.Contains(err.Error(), "is unavailable in the disk db"), true,
			"Error not created if database doesn't have a node")
	}
}

// nolint: gosec
// test only code
func getFreshData(size int) [][]byte {
	length := 8
	var data [][]byte
	for i := 0; i < size; i++ {
		key := make([]byte, 8)
		_, err := rand.Read(key)
		if err != nil {
			panic(err)
		}
		data = append(data, hasher(key)[:length])
	}
	sort.Sort(dataArray(data))
	return data
}

func benchmark10MAccounts10Ktps(smt *smt, b *testing.B) {
	fmt.Println("\nLoading b.N x 1000 accounts")
	for index := 0; index < b.N; index++ {
		newkeys := getFreshData(1000)
		newvalues := getFreshData(1000)
		start := time.Now()
		smt.Update(newkeys, newvalues)
		end := time.Now()
		end2 := time.Now()
		for i, key := range newkeys {
			val, _ := smt.Get(key)
			if !bytes.Equal(val, newvalues[i]) {
				b.Fatal("new key not included")
			}
		}
		end3 := time.Now()
		elapsed := end.Sub(start)
		elapsed2 := end2.Sub(end)
		elapsed3 := end3.Sub(end2)
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		fmt.Println(index, " : update time : ", elapsed, "commit time : ", elapsed2,
			"\n1000 Get time : ", elapsed3,
			"\nRAM : ", m.Sys/1024/1024, " MiB")
	}
}

// go test -run=xxx -bench=. -benchmem -test.benchtime=20s
func BenchmarkCacheHeightLimit(b *testing.B) {
	smt := newSMT(hasher, cache.NewTTL(forever, time.Minute), time.Minute)
	benchmark10MAccounts10Ktps(smt, b)
}
