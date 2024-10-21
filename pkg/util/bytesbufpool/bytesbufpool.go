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

package bytesbufpool

import (
	"bytes"
	"sync"
)

// BufferPool is a simple wrapper around bytes.Buffer pool, used when buffers need to be
// allocated repeatedly in a short period of time. It uses sync.Pool to cache allocated but
// no longer used buffers for later reuse, thus reducing pressure on the garbage collector.

type BufferPool struct {
	cap  int // the bytes.Buffer minimum capacity
	pool sync.Pool
}

// New create a buffer pool.
func New() *BufferPool {
	return NewWithCap(64)
}

// NewWithCap create a buffer pool and specifies the `Get` method returned buffer's minimum capacity.
func NewWithCap(n int) *BufferPool {
	return &BufferPool{
		cap:  n,
		pool: sync.Pool{},
	}
}

// Get a buffer from the pool.
func (p *BufferPool) Get() *bytes.Buffer {
	return p.GetWithCap(p.cap)
}

// GetWithCap get a buffer of a specified the minimum capacity from the pool.
func (p *BufferPool) GetWithCap(n int) *bytes.Buffer {
	val := p.pool.Get()
	if val != nil {
		buf, _ := val.(*bytes.Buffer)
		if buf.Cap() < n {
			buf.Grow(n)
		}
		return buf
	}

	if n < p.cap {
		n = p.cap
	}
	return bytes.NewBuffer(make([]byte, 0, n))
}

// Put the buffer back in the pool.
func (p *BufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	// Proper usage of a sync.BufferPool requires each entry to have approximately
	// the same memory cost. To obtain this property when the stored type
	// contains a variably-sized buffer, we add a hard limit on the maximum
	// buffer to put back in the pool. If the buffer is larger than the
	// limit, we drop the buffer and not put back.
	if buf.Cap() >= 64*1024 {
		buf = nil
		return
	}

	buf.Reset()
	p.pool.Put(buf)
}
