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

package event

import (
	"sync"

	"istio.io/istio/galley/pkg/config/scope"
)

// Buffer is a growing event buffer.
type Buffer struct {
	mu         sync.Mutex
	queue      queue
	handler    Handler
	cond       *sync.Cond
	processing bool
}

var _ Handler = &Buffer{}
var _ Dispatcher = &Buffer{}

// NewBuffer returns new Buffer instance
func NewBuffer() *Buffer {
	b := &Buffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// WithBuffer returns a new Buffer instance that listens to the given Source.
func WithBuffer(s Dispatcher) *Buffer {
	b := NewBuffer()
	s.Dispatch(b)

	return b
}

// Handle implements Handler
func (b *Buffer) Handle(e Event) {
	b.mu.Lock()
	b.queue.add(e)
	b.cond.Broadcast()
	b.mu.Unlock()
}

// Dispatch implements Source
func (b *Buffer) Dispatch(handler Handler) {
	b.handler = CombineHandlers(b.handler, handler)
}

// Clear the buffer contents.
func (b *Buffer) Clear() {
	b.mu.Lock()
	b.queue.clear()
	b.mu.Unlock()
}

// Stop processing
func (b *Buffer) Stop() {
	b.mu.Lock()
	b.processing = false
	b.cond.Broadcast()
	b.mu.Unlock()
}

// Process events in the buffer. This method will not return until the Buffer is stopped.
func (b *Buffer) Process() {
	b.mu.Lock()
	if b.processing {
		b.mu.Unlock()
		return
	}
	b.processing = true

	for {
		// lock must be held when entering the for loop (whether from beginning, or through loop continuation).
		// this makes locking/unlocking slightly more efficient.
		if !b.processing {
			scope.Processing.Debug(">>> Buffer.Process: exiting")
			b.mu.Unlock()
			return
		}

		e, ok := b.queue.pop()
		if !ok {
			scope.Processing.Debug("Buffer.Process: no more items to process, waiting")
			b.cond.Wait()
			continue
		}

		if b.handler != nil {
			b.mu.Unlock()
			b.handler.Handle(e)
			b.mu.Lock()
		}
	}
}
