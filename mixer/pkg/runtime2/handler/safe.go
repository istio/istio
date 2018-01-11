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

package handler

import (
	"context"
	"fmt"

	"istio.io/istio/mixer/pkg/adapter"
)

// safeHandlerBuilder is a wrapper around adapter.Builder and provides panic protection.
type safeHandlerBuilder struct {
	b adapter.HandlerBuilder
}

// SafeHandler is a wrapper around adapter.Handler and provides panic protection.
type SafeHandler struct {
	h adapter.Handler
}

// SetAdapterConfig calls the same-named method on the wrapped builder. Returns error if the call panics.
func (s safeHandlerBuilder) SetAdapterConfig(c adapter.Config) (err error) {
	// Try to detect panic, even if panic was called with nil.
	reachedEnd := false
	defer func() {
		if reachedEnd {
			return
		}

		r := recover()
		err = fmt.Errorf("panic during HandlerBuilder.SetAdapter: '%v' ", r)
	}()

	s.b.SetAdapterConfig(c)
	err = nil
	reachedEnd = true
	return
}

// Validate calls the same-named method on the wrapped builder. Returns either the result of the call and nil
// error, or nil adapter.ConfigError and error that is the result of a panic.
func (s safeHandlerBuilder) Validate() (cerr *adapter.ConfigErrors, err error) {
	// Try to detect panic, even if panic was called with nil.
	reachedEnd := false

	defer func() {
		if reachedEnd {
			return
		}

		r := recover()
		err = fmt.Errorf("panic during HandlerBuilder.Validate: '%v' ", r)
	}()

	cerr = s.b.Validate()
	reachedEnd = true
	return
}

// Build calls the same-named method on the wrapped builder. Returns either the resulf of the call, or
// (nil, error) if there was a panic.
func (s safeHandlerBuilder) Build(c context.Context, e adapter.Env) (handler SafeHandler, err error) {
	// Try to detect panic, even if panic was called with nil.
	reachedEnd := false

	defer func() {
		if reachedEnd {
			return
		}

		handler = SafeHandler{}
		r := recover()
		err = fmt.Errorf("panic during HandlerBuilder.Build: '%v' ", r)
	}()

	var h adapter.Handler
	h, err = s.b.Build(c, e)
	handler = SafeHandler{h}
	reachedEnd = true
	return
}

// Close calls the same-named method on the wrapped handler. Returns either the result of the call, or
// an error if there was a panic.
func (s SafeHandler) Close() (err error) {
	// Try to detect panic, even if panic was called with nil.
	reachedEnd := false

	defer func() {
		if reachedEnd {
			return
		}

		r := recover()
		err = fmt.Errorf("panic during Handler.Close: '%v'", r)

	}()

	err = s.h.Close()
	reachedEnd = true
	return
}

// IsValid returns true if the SafeHandler is initialized and holds a reference to a valid adapter.Handler.
func (s SafeHandler) IsValid() bool {
	return s.h != nil
}
