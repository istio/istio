// Copyright 2018 Istio Authors
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

// Copyright (c) 2016 Aliaksandr Valialkin, VertaMedia, Refactor by Andy Pan. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found
// at https://github.com/valyala/bytebufferpool/blob/master/LICENSE.

package yaml

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/gogo/protobuf/proto"
)

const (
	minBitSize = 6 // 2**6=64 is a CPU cache line size
	steps      = 20

	minSize = 1 << minBitSize

	calibrateCallsThreshold = 42000
	maxPercentile           = 0.95
)

// protoBufferPool represents proto.Buffer pool.
//
// Distinct pools may be used for distinct types of byte buffers.
// Properly determined proto.Buffer types with their own pools may help reducing
// memory waste.
type protoBufferPool struct {
	calls       [steps]uint64
	calibrating uint64

	defaultSize uint64
	maxSize     uint64

	pool sync.Pool
}

var defaultPool protoBufferPool

func init() {
	defaultPool.defaultSize = 256
}

// GetBuffer returns an empty proto.Buffer from the pool.
//
// Got proto.Buffer may be returned to the pool via Put call.
// This reduces the number of memory allocations required for proto.Buffer
// management.
func GetBuffer() *proto.Buffer { return defaultPool.Get() }

// Get returns new proto.Buffer with zero length.
//
// The proto.Buffer may be returned to the pool via Put after the use
// in order to minimize GC overhead.
func (p *protoBufferPool) Get() *proto.Buffer {
	v := p.pool.Get()
	if v != nil {
		return v.(*proto.Buffer)
	}
	return proto.NewBuffer(make([]byte, 0, atomic.LoadUint64(&p.defaultSize)))
}

// PutBuffer returns proto.Buffer to the pool.
//
// ByteBuffer.B mustn't be touched after returning it to the pool.
// Otherwise data races will occur.
func PutBuffer(b *proto.Buffer) { defaultPool.Put(b) }

// Put releases proto.Buffer obtained via Get to the pool.
//
// The buffer mustn't be accessed after returning to the pool.
func (p *protoBufferPool) Put(b *proto.Buffer) {
	idx := index(len(b.Bytes()))

	if atomic.AddUint64(&p.calls[idx], 1) > calibrateCallsThreshold {
		p.calibrate()
	}

	maxSize := int(atomic.LoadUint64(&p.maxSize))
	if maxSize == 0 || cap(b.Bytes()) <= maxSize {
		b.Reset()
		p.pool.Put(b)
	}
}

func (p *protoBufferPool) calibrate() {
	if !atomic.CompareAndSwapUint64(&p.calibrating, 0, 1) {
		return
	}

	a := make(callSizes, 0, steps)
	var callsSum uint64
	for i := uint64(0); i < steps; i++ {
		calls := atomic.SwapUint64(&p.calls[i], 0)
		callsSum += calls
		a = append(a, callSize{
			calls: calls,
			size:  minSize << i,
		})
	}
	sort.Sort(a)

	defaultSize := a[0].size
	maxSize := defaultSize

	maxSum := uint64(float64(callsSum) * maxPercentile)
	callsSum = 0
	for i := 0; i < steps; i++ {
		if callsSum > maxSum {
			break
		}
		callsSum += a[i].calls
		size := a[i].size
		if size > maxSize {
			maxSize = size
		}
	}

	atomic.StoreUint64(&p.defaultSize, defaultSize)
	atomic.StoreUint64(&p.maxSize, maxSize)

	atomic.StoreUint64(&p.calibrating, 0)
}

type callSize struct {
	calls uint64
	size  uint64
}

type callSizes []callSize

func (ci callSizes) Len() int {
	return len(ci)
}

func (ci callSizes) Less(i, j int) bool {
	return ci[i].calls > ci[j].calls
}

func (ci callSizes) Swap(i, j int) {
	ci[i], ci[j] = ci[j], ci[i]
}

func index(n int) int {
	n--
	n >>= minBitSize
	idx := 0
	for n > 0 {
		n >>= 1
		idx++
	}
	if idx >= steps {
		idx = steps - 1
	}
	return idx
}
