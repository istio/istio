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
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"istio.io/api/mixer/adapter/model/v1beta1"
	policypb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/attribute"
	"github.com/gogo/protobuf/types"
)

type (

	// Info describes the Adapter and provides a function to a Handler Builder method.
	Info struct {
		// Name returns the official name of the adapter, it must be RFC 1035 compatible DNS label.
		// Regex: "^[a-z]([-a-z0-9]*[a-z0-9])?$"
		// Name is used in Istio configuration, therefore it should be descriptive but short.
		// example: denier
		// Vendor adapters should use a vendor prefix.
		// example: mycompany-denier
		Name string
		// Impl is the package implementing the adapter.
		// example: "istio.io/istio/mixer/adapter/denier"
		Impl string
		// Description returns a user-friendly description of the adapter.
		Description string
		// NewBuilder is a function that creates a Builder which implements Builders associated
		// with the SupportedTemplates.
		NewBuilder NewBuilderFn
		// SupportedTemplates expressess all the templates the Adapter wants to serve.
		SupportedTemplates []string
		// DefaultConfig is a default configuration struct for this
		// adapter. This will be used by the configuration system to establish
		// the shape of the block of configuration state passed to the HandlerBuilder.Build method.
		DefaultConfig proto.Message
	}

	// Dynamic contains adapter info about a dynamic adapter
	Dynamic struct {
		Name string

		// Adapter's file descriptor set.
		ConfigDescSet *descriptor.FileDescriptorSet

		// package name of the `Params` message
		PackageName string

		SupportedTemplates []*DynamicTemplate

		SessionBased bool

		Description string
	}


	// DynamicTemplate contains info about a Dynamic Template associated with an adapter
	DynamicTemplate struct {
		// Name of the template.
		//
		// Note this is the template's resource name and not the template's internal name that adapter developer
		// uses to implement adapter service.
		Name string

		// Variety of this template
		Variety v1beta1.TemplateVariety

		// InternalPackageDerivedName is the name of the template from adapter developer point of view.
		// The service and functions implemented by the adapter is based on this name
		// NOTE: This name derived from template proto package and not the resource name.
		InternalPackageDerivedName string

		// Template's file descriptor set.
		// This is the descriptor set that is produced from the *_service proto.
		// It includes grpc service and template protos.
		FileDescSet *descriptor.FileDescriptorSet

		// package name of the `Template` message
		PackageName string
	}

	// DynamicInstance configuration for dynamically loaded templates. Fully resolved.
	DynamicInstance struct {
		Name string

		Template *DynamicTemplate

		Encoder Encoder

		// Params used to create the Encoder.
		Params map[string]interface{}
	}

	// DynamicHandler configuration for dynamically loaded, grpc adapters. Fully resolved.
	DynamicHandler struct {
		Name string

		Adapter *Dynamic

		// AdapterConfig used to construct the Handler. This is passed in verbatim to the remote adapter.
		AdapterConfig *types.Any

		// Connection information for the handler.
		Connection *policypb.Connection
	}


	Encoder interface {
		Encode(bag attribute.Bag, ba []byte) ([]byte, error)
	}

)

// NewBuilderFn is a function that creates a Builder.
type NewBuilderFn func() HandlerBuilder

// InfoFn returns an AdapterInfo object that Mixer will use to create HandlerBuilder
type InfoFn func() Info
