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

// Package ledger implements a modified map with three unique characteristics:
// 1. every unique state of the map is given a unique hash
// 2. prior states of the map are retained for a fixed period of time
// 2. given a previous hash, we can retrieve a previous state from the map, if it is still retained.
package ledger

import (
	"encoding/base64"
	"time"

	"github.com/spaolacci/murmur3"
)

// Ledger exposes a modified map with three unique characteristics:
// 1. every unique state of the map is given a unique hash
// 2. prior states of the map are retained for a fixed period of time
// 2. given a previous hash, we can retrieve a previous state from the map, if it is still retained.
type Ledger interface {
	// Put adds or overwrites a key in the Ledger
	Put(key, value string) (string, error)
	// Delete removes a key from the Ledger, which may still be read using GetPreviousValue
	Delete(key string) error
	// Get returns a the value of the key from the Ledger's current state
	Get(key string) (string, error)
	// RootHash is the hash of all keys and values currently in the Ledger
	RootHash() string
	// GetPreviousValue executes a get against a previous version of the ledger, using that version's root hash.
	GetPreviousValue(previousRootHash, key string) (result string, err error)
}

type smtLedger struct {
	tree *smt
}

// Make returns a Ledger which will retain previous nodes after they are deleted.
func Make(retention time.Duration) Ledger {
	return smtLedger{tree: newSMT(hasher, nil, retention)}
}

// Put adds a key value pair to the ledger, overwriting previous values and marking them for
// removal after the retention specified in Make()
func (s smtLedger) Put(key, value string) (result string, err error) {
	b, err := s.tree.Update([][]byte{coerceKeyToHashLen(key)}, [][]byte{coerceToHashLen(value)})
	result = string(b)
	return
}

// Delete removes a key value pair from the ledger, marking it for removal after the retention specified in Make()
func (s smtLedger) Delete(key string) (err error) {
	_, err = s.tree.Update([][]byte{coerceKeyToHashLen(key)}, [][]byte{defaultLeaf})
	return
}

// GetPreviousValue returns the value of key when the ledger's RootHash was previousHash, if it is still retained.
func (s smtLedger) GetPreviousValue(previousRootHash, key string) (result string, err error) {
	prevBytes, err := base64.StdEncoding.DecodeString(previousRootHash)
	if err != nil {
		return "", err
	}
	b, err := s.tree.GetPreviousValue(prevBytes, coerceKeyToHashLen(key))
	var i int
	// trim leading 0's from b
	for i = range b {
		if b[i] != 0 {
			break
		}
	}
	result = string(b[i:])
	return
}

// Get returns the current value of key.
func (s smtLedger) Get(key string) (result string, err error) {
	return s.GetPreviousValue(s.RootHash(), key)
}

// RootHash represents the hash of the current state of the ledger.
func (s smtLedger) RootHash() string {
	return base64.StdEncoding.EncodeToString(s.tree.Root())
}

func coerceKeyToHashLen(val string) []byte {
	hasher := murmur3.New64()
	_, _ = hasher.Write([]byte(val))
	return hasher.Sum(nil)
}

func coerceToHashLen(val string) []byte {
	// hash length is fixed at 64 bits until generic support is added
	const hashLen = 64
	byteVal := []byte(val)
	if len(byteVal) < hashLen/8 {
		// zero fill the left side of the slice
		zerofill := make([]byte, hashLen/8)
		byteVal = append(zerofill[:hashLen/8-len(byteVal)], byteVal...)
	}
	return byteVal[:hashLen/8]
}
