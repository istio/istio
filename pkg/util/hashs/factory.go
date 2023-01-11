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

package hashs

import (
	"encoding/binary"
	"encoding/hex"
	"hash"

	"github.com/cespare/xxhash/v2"
)

type Instance struct {
	hash.Hash
}

func New() *Instance {
	return &Instance{
		xxhash.New(),
	}
}

// Write wraps the Hash.Write function call
// Hash.Write error always return nil, this func simplify caller handle error
func (i *Instance) Write(p []byte) (n int) {
	n, _ = i.Hash.Write(p)
	return
}

func (i *Instance) ToUint64(b []byte) uint64 {
	sum := i.Sum(b)
	return binary.LittleEndian.Uint64(sum)
}

func (i *Instance) ToString(b []byte) string {
	sum := i.Sum(b)
	return hex.EncodeToString(sum)
}
