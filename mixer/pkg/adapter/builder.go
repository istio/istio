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

package adapter

import (
	"io"
)

// BuilderConfig is used to configure a builder.
type BuilderConfig interface{}

// Builder represents the factory that the mixer uses to create individual adapter.
type Builder interface {
	io.Closer

	// Name returns the official name of this builder's adapters for use in diagnostics and in config
	Name() string

	// Description returns a user-friendly description of this builder's adapters.
	Description() string

	// DefaultBuilderConfig returns a default configuration struct for this builder.
	// This will be used by the configuration system to establish the shape of the block
	// of configuration state passed to the Configure function.
	DefaultBuilderConfig() BuilderConfig

	// ValidateBuilderConfig determines whether the given configuration meets all correctness requirements.
	ValidateBuilderConfig(config BuilderConfig) error

	// Configures prepares the builder with the given configuration. Once the builder has been configured,
	// the mixer can start calling the NewAspect method to instantiate adapters. A given builder is only
	// ever configured once in its lifetime.
	Configure(config BuilderConfig) error

	// DefaultAspectConfig returns a default configuration struct for this builder's
	// adapters. This will be used by the configuration system to establish
	// the shape of the block of configuration state passed to the NewAspect method.
	DefaultAspectConfig() AspectConfig

	// ValidateAspectConfig determines whether the given configuration meets all correctness requirements.
	ValidateAspectConfig(config AspectConfig) error

	// NewAspect creates a single aspect based on the supplied configuration.
	NewAspect(config AspectConfig) (Aspect, error)
}
