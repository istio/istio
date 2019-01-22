//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"context"
	"sync"
)

// admitter is a basic shunting synchronization primitive, that allows entry/exit as long as it is allowed.
type admitter struct {
	current  uint64
	mu       sync.Mutex
	disallow bool
	err      error

	context context.Context
	cancel  context.CancelFunc
}

func newAdmitter(parent context.Context) *admitter {
	ctx, cancel := context.WithCancel(parent)
	return &admitter{
		context: ctx,
		cancel:  cancel,
	}
}

// enter if admittance is still allowed. Returns true if admitted. Caller must call leave() to indicate
// their departure.
func (a *admitter) enter() bool {
	a.mu.Lock()
	allowed := !a.disallow
	if allowed {
		a.current++
	}
	a.mu.Unlock()
	return allowed
}

func (a *admitter) leave() {
	a.mu.Lock()
	if a.current > 0 {
		a.current--
	}
	if a.current == 0 && a.disallow {
		a.cancel()
	}
	a.mu.Unlock()
}

// block admittance. Caller can use wait() method to wait until all admitted calls are drained.
func (a *admitter) block() {
	a.mu.Lock()
	a.disallow = true
	a.mu.Unlock()
}

func (a *admitter) abort(err error) {
	a.mu.Lock()
	if err != nil && a.err == nil {
		a.err = err
	}
	a.disallow = true
	a.cancel()
	a.mu.Unlock()
}

// wait until drained or aborted. This should be called after calling block, otherwise there is no guarantee
// that there will be no additional admittance after wait returns.
func (a *admitter) wait() error {
	<-a.context.Done()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}
