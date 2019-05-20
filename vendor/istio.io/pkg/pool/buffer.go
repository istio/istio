// Copyright 2016 Istio Authors
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

// Package pool provides access to a mixer-global pool of buffers, a pool of goroutines, and
// a string interning table.
package pool

import (
	"bytes"
	"sync"
)

var bufferPool = sync.Pool{New: func() interface{} { return new(bytes.Buffer) }}

// GetBuffer returns a buffer from the buffer pool.
func GetBuffer() *bytes.Buffer {
	return bufferPool.Get().(*bytes.Buffer)
}

// PutBuffer returns a buffer to the buffer pool. You shouldn't reference this buffer
// after it has been returned to the pool, otherwise bad things will happen.
func PutBuffer(b *bytes.Buffer) {
	b.Reset()
	bufferPool.Put(b)
}
