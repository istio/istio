// Copyright (c) 2012-2016 Eli Janssen
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package statsd

import (
	"bytes"
	"sync"
)

type bufferPool struct {
	*sync.Pool
}

func newBufferPool() *bufferPool {
	return &bufferPool{
		&sync.Pool{New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 1700))
		}},
	}
}

func (bp *bufferPool) Get() *bytes.Buffer {
	return (bp.Pool.Get()).(*bytes.Buffer)
}

func (bp *bufferPool) Put(b *bytes.Buffer) {
	b.Truncate(0)
	bp.Pool.Put(b)
}
