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

// Package aspect contains the various aspect managers which are responsible for
// mapping incoming requests into the interface expected by individual types of
// aspects.
package aspect

import (
	"io"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
)

type (
	// Output captures the output from invoking an aspect.
	Output struct {
		// status code
		Status rpc.Status

		//TODO attribute mutator
		//If any attributes should change in the context for the next call
		//context remains immutable during the call
	}

	// Manager is responsible for a specific aspect and presents a uniform interface
	// to the rest of the system.
	Manager interface {
		adapter.ConfigValidator

		// NewAspect creates a new aspect instance given configuration.
		NewAspect(cfg *config.Combined, adapter adapter.Builder, env adapter.Env) (Wrapper, error)

		// Kind return the kind of aspect
		Kind() Kind
	}

	// Wrapper encapsulates a single aspect and allows it to be invoked.
	Wrapper interface {
		io.Closer

		// Execute dispatches to the adapter.
		Execute(attrs attribute.Bag, mapper expr.Evaluator, ma APIMethodArgs) Output
	}
)

// IsOK returns whether the Output represents success or failure
func (o Output) IsOK() bool {
	return status.IsOK(o.Status)
}

// Message returns te the message string of the Output's embedded Status struct
func (o Output) Message() string {
	return o.Status.Message
}
