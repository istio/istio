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

	"github.com/spaolacci/murmur3"
)

// defaultLeaf is the Trie default value : hash of 0x0
var defaultLeaf = hasher([]byte{0x0})

const (
	hashLength = 8
)

type hash [hashLength]byte

func bitIsSet(bits []byte, i int) bool {
	return bits[i/8]&(1<<uint(7-i%8)) != 0
}

func hasher(data ...[]byte) []byte {
	hasher := murmur3.New64()
	for i := 0; i < len(data); i++ {
		_, _ = hasher.Write(data[i])
	}
	result := hasher.Sum(nil)
	return result
}

// for sorting
type dataArray [][]byte

func (d dataArray) Len() int {
	return len(d)
}

func (d dataArray) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}

func (d dataArray) Less(i, j int) bool {
	return bytes.Compare(d[i], d[j]) == -1
}
