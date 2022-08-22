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

// Package lazy is a package to expose lazily computed values.
// The concepts and code are heavily influenced by https://cs.opensource.google/go/go/+/go1.19:src/sync/once.go.
package lazy

import (
	"sync"
	"sync/atomic"
)

// Lazy represents a value whose computation is deferred until the first access
type Lazy[T any] interface {
	// Get returns the value, computing it if necessary.
	Get() (T, error)
}

type lazyImpl[T any] struct {
	getter func() (T, error)
	// retry, if true, will ensure getter() is called for each Get() until a non-nil error is returned.
	retry bool

	// Cached responses. Note: with retry enabled, this will be unset until a non-nil error
	res T
	err error

	done uint32
	m    sync.Mutex
}

var _ Lazy[any] = &lazyImpl[any]{}

// New returns a new lazily computed value. The value is guaranteed to only be computed a single time.
func New[T any](f func() (T, error)) Lazy[T] {
	return &lazyImpl[T]{getter: f}
}

// NewWithRetry returns a new lazily computed value. The value will be computed on each call until a
// non-nil error is returned.
func NewWithRetry[T any](f func() (T, error)) Lazy[T] {
	return &lazyImpl[T]{getter: f, retry: true}
}

func (l *lazyImpl[T]) Get() (T, error) {
	if atomic.LoadUint32(&l.done) == 0 {
		// Outlined slow-path to allow inlining of the fast-path.
		return l.doSlow()
	}
	return l.res, l.err
}

func (l *lazyImpl[T]) doSlow() (T, error) {
	l.m.Lock()
	defer l.m.Unlock()
	if l.done == 0 {
		done := uint32(1)
		// Defer in case of panic
		defer func() {
			atomic.StoreUint32(&l.done, done)
		}()
		res, err := l.getter()
		if l.retry && err != nil {
			done = 0
		} else {
			l.res, l.err = res, err
		}
		return res, err
	}
	return l.res, l.err
}
