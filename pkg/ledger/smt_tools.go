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
)

// Get fetches the value of a key by going down the current trie root.
func (s *smt) Get(key []byte) ([]byte, error) {
	return s.GetPreviousValue(s.Root(), key)
}

// GetPreviousValue returns the value as of the specified root hash.
func (s *smt) GetPreviousValue(prevRoot []byte, key []byte) ([]byte, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	s.atomicUpdate = false
	return s.get(prevRoot, key, nil, 0, s.trieHeight)
}

// get fetches the value of a key given a trie root
func (s *smt) get(root []byte, key []byte, batch [][]byte, iBatch, height int) ([]byte, error) {
	if len(root) == 0 {
		return nil, nil
	}
	if height == 0 {
		return root[:hashLength], nil
	}
	// Fetch the children of the node
	batch, iBatch, lnode, rnode, isShortcut, err := s.loadChildren(root, height, iBatch, batch)
	if err != nil {
		return nil, err
	}
	if isShortcut {
		if bytes.Equal(lnode[:hashLength], key) {
			return rnode[:hashLength], nil
		}
		return nil, nil
	}
	if bitIsSet(key, s.trieHeight-height) {
		// visit right node
		return s.get(rnode, key, batch, 2*iBatch+2, height-1)
	}
	// visit left node
	return s.get(lnode, key, batch, 2*iBatch+1, height-1)
}

// DefaultHash is a getter for the defaultHashes array
func (s *smt) DefaultHash(height int) []byte {
	return s.defaultHashes[height]
}
