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

package handler

import (
	"context"

	"github.com/gogo/protobuf/proto"

	"istio.io/mixer/pkg/adapter"
)

// Info describes the Adapter and provides a function to a Handler Builder method.
// TODO change this to Info when we delete the ApplicationLog.Info enum.
type Info struct {
	// Name returns the official name of the adapter, it must be RFC 1035 compatible DNS label.
	// Regex: "^[a-z]([-a-z0-9]*[a-z0-9])?$"
	// Name is used in Istio configuration, therefore it should be descriptive but short.
	// example: denier
	// Vendor adapters should use a vendor prefix.
	// example: mycompany-denier
	Name string
	// Impl is the package implementing the adapter.
	// example: "istio.io/mixer/adapter/denier"
	Impl string
	// Description returns a user-friendly description of the adapter.
	Description string
	// CreateHandlerBuilder is a function that creates a HandlerBuilder which implements Builders associated
	// with the SupportedTemplates.
	CreateHandlerBuilder CreateHandlerBuilderFn // DEPRECATED
	// NewBuilder is a function that creates a Builder which implements Builders associated
	// with the SupportedTemplates.
	NewBuilder NewBuilderFn
	// SupportedTemplates expressess all the templates the Adapter wants to serve.
	SupportedTemplates []string
	// DefaultConfig is a default configuration struct for this
	// adapter. This will be used by the configuration system to establish
	// the shape of the block of configuration state passed to the HandlerBuilder.Build method.
	DefaultConfig proto.Message
	// ValidateConfig is a function that determines whether the given handler configuration meets all
	// correctness requirements.
	ValidateConfig ValidateConfigFn // DEPRECATED

	// ValidateConfig2 is a function that determines whether the given handler configuration meets all
	// correctness requirements.
	ValidateConfig2 ValidateConfigFn2

	// NewHandler must return a handler that implements all the template-specific runtime request serving
	// interfaces that the adapter supports.
	// If the returned Handler fails to implement the required interfaces, Mixer will report an error and stop serving
	// runtime traffic to the particular Handler.
	NewHandler NewHandlerFn
}

// CreateHandlerBuilderFn is a function that creates a HandlerBuilder.
type CreateHandlerBuilderFn func() adapter.HandlerBuilder

// NewBuilderFn is a function that creates a Builder.
type NewBuilderFn func() adapter.Builder2

// NewHandlerFn must return a handler that implements all the template-specific runtime request serving
// interfaces that adapter supports.
// If the returned Handler fails to implement the required interfaces, Mixer will report an error and stop serving
// runtime traffic to the particular Handler.
type NewHandlerFn func(context.Context, adapter.Env, *HandlerConfig) (adapter.Handler, error)

// ValidateConfigFn2 is a function that determines whether the given handler configuration meets all
// correctness requirements.
type ValidateConfigFn2 func(*HandlerConfig) *adapter.ConfigErrors

// ValidateConfigFn is a function that determines whether the given handler configuration meets all
// correctness requirements.
type ValidateConfigFn func(adapter.Config) *adapter.ConfigErrors

// InfoFn returns an AdapterInfo object that Mixer will use to create HandlerBuilder
type InfoFn func() Info
