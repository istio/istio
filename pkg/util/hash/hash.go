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

package hash

import (
	"encoding/hex"

	"github.com/cespare/xxhash/v2"
)

type Hash interface {
	Write(p []byte) (n int)
	WriteString(s string) (n int)
	Sum() string
	Sum64() uint64
}

type instance struct {
	hash *xxhash.Digest
}

var _ Hash = &instance{}

func New() Hash {
	return &instance{
		hash: xxhash.New(),
	}
}

// Write wraps the Hash.Write function call
// Hash.Write error always return nil, this func simplify caller handle error
func (i *instance) Write(p []byte) (n int) {
	n, _ = i.hash.Write(p)
	return n
}

// Write wraps the Hash.Write function call
// Hash.Write error always return nil, this func simplify caller handle error
func (i *instance) WriteString(s string) (n int) {
	n, _ = i.hash.WriteString(s)
	return n
}

func (i *instance) Sum64() uint64 {
	return i.hash.Sum64()
}

func (i *instance) Sum() string {
	sum := i.hash.Sum(nil)
	return hex.EncodeToString(sum)
}
