// Copyright 2016 Google Inc.
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
	"google.golang.org/genproto/googleapis/rpc/code"

	istioconfig "istio.io/api/mixer/v1/config"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

type (
	// CombinedConfig combines all configuration related to an aspect.
	CombinedConfig struct {
		Aspect  *istioconfig.Aspect
		Adapter *istioconfig.Adapter
	}

	// Output captures the output from invoking an aspect.
	Output struct {
		// status code
		Code code.Code
		//TODO attribute mutator
		//If any attributes should change in the context for the next call
		//context remains immutable during the call
	}

	// Manager is responsible for a specific aspect and presents a uniform interface
	// to the rest of the system.
	Manager interface {
		adapter.ConfigValidator

		// NewAspect creates a new aspect instance given configuration.
		NewAspect(cfg *CombinedConfig, adapter adapter.Adapter, env adapter.Env) (Wrapper, error)

		// Kind return the kind of aspect
		Kind() string
	}

	// Wrapper encapsulates a single aspect and allows it to be invoked.
	Wrapper interface {
		// Execute dispatches to the given adapter.
		Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error)
	}
)
