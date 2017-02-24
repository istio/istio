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

// Package adapter defines the types consumed by adapter implementations to
// interface with the mixer.
package adapter

import (
	"io"

	"github.com/golang/protobuf/proto"
)

type (
	// Aspect represents a type of end-user functionality with particular semantics. Adapters
	// expose functionality to the mixer by implementing one or more aspects.
	Aspect interface {
		io.Closer
	}

	// Builder represents a factory of aspects. Adapters register builders with the mixer
	// in order to allow the mixer to instantiate aspects on demand.
	Builder interface {
		io.Closer
		ConfigValidator

		// Name returns the official name of the aspects produced by this builder.
		Name() string

		// Description returns a user-friendly description of the aspects produced by this builder.
		Description() string
	}

	// AspectConfig represents a chunk of configuration state
	AspectConfig proto.Message

	// ConfigValidator handles adapter configuration defaults and validation.
	ConfigValidator interface {
		// DefaultConfig returns a default configuration struct for this
		// adapter. This will be used by the configuration system to establish
		// the shape of the block of configuration state passed to the NewAspect method.
		DefaultConfig() (c AspectConfig)

		// ValidateConfig determines whether the given configuration meets all correctness requirements.
		ValidateConfig(c AspectConfig) *ConfigErrors
	}

	// Env defines the environment in which an aspect executes.
	Env interface {
		// Logger returns the logger for the aspect to use at runtime.
		Logger() Logger

		// Possible other things:
		// Return how much time remains until the mixer considers the aspect call having timed out and kills it
		// Return true/false to indicate this is a 'recovery mode' execution following a prior crash of the aspect
		// ?
	}

	// Logger defines where aspects should output their log state to.
	//
	// This log information is funneled to the mixer which
	// augments it with desirable metadata and then routes it
	// to the right place.
	Logger interface {
		// Infof logs optional information.
		Infof(format string, args ...interface{})

		// Warningf logs suspect situations and recoverable errors
		Warningf(format string, args ...interface{})

		// Errorf logs error conditions.
		// In addition to generating a log record for the error, this also returns
		// an error instance for convenience.
		Errorf(format string, args ...interface{}) error
	}
)
