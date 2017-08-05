// Copyright 2017 Istio Authors.
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
	"github.com/golang/protobuf/proto"
)

// BuilderInfo describes the Adapter and provides a function to a Handler Builder method.
type BuilderInfo struct {
	// Name returns the official name of the adapter.
	Name string
	// Description returns a user-friendly description of the adapter.
	Description string
	// CreateHandlerBuilderFn is a function that creates a HandlerBuilder which implements Builders associated
	// with the SupportedTemplates.
	CreateHandlerBuilderFn CreateHandlerBuilder
	// SupportedTemplates expressess all the templates the Adapter wants to serve.
	SupportedTemplates []string
	// DefaultConfig is a default configuration struct for this
	// adapter. This will be used by the configuration system to establish
	// the shape of the block of configuration state passed to the HandlerBuilder.Build method.
	DefaultConfig proto.Message
	// ValidateConfig is a function that determines whether the given handler configuration meets all
	// correctness requirements.
	ValidateConfig ValidateConfig
}

// CreateHandlerBuilder is a function that creates a HandlerBuilder.
type CreateHandlerBuilder func() HandlerBuilder

// ValidateConfig is a function that determines whether the given handler configuration meets all
// correctness requirements.
type ValidateConfig func(proto.Message) error

// GetBuilderInfoFn returns an BuilderInfo object that Mixer will use to create HandlerBuilder
type GetBuilderInfoFn func() BuilderInfo
