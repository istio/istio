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

package config

import (
	"context"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/types"

	adptTmpl "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/protobuf/yaml/dynamic"
	"istio.io/istio/mixer/pkg/runtime/lang"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/attribute"
)

type (

	// Snapshot view of configuration.
	Snapshot struct {
		ID int64

		// Static information
		Templates map[string]*template.Info
		Adapters  map[string]*adapter.Info

		// Config store based information
		Attributes attribute.AttributeDescriptorFinder

		HandlersStatic  map[string]*HandlerStatic
		InstancesStatic map[string]*InstanceStatic

		//  TemplateMetadatas contains template descriptors loaded from the store
		TemplateMetadatas map[string]*Template
		//  AdapterMetadatas contains adapter metadata loaded from the store
		AdapterMetadatas map[string]*Adapter

		HandlersDynamic  map[string]*HandlerDynamic
		InstancesDynamic map[string]*InstanceDynamic
		Rules            []*Rule

		// Used to update Perf measures relevant to configuration.
		MonitoringContext context.Context
	}

	// HandlerDynamic configuration for dynamically loaded, grpc adapters. Fully resolved.
	HandlerDynamic struct {
		Name string

		Adapter *Adapter

		// AdapterConfig used to construct the Handler. This is passed in verbatim to the remote adapter.
		AdapterConfig *types.Any

		// Connection information for the handler.
		Connection *v1beta1.Connection
	}

	// HandlerStatic configuration for compiled in adapters. Fully resolved.
	HandlerStatic struct {

		// Name of the Handler. Fully qualified.
		Name string

		// Associated adapter. Always resolved.
		Adapter *adapter.Info

		// parameters used to construct the Handler.
		Params proto.Message
	}

	// InstanceDynamic configuration for dynamically loaded templates. Fully resolved.
	InstanceDynamic struct {
		Name string

		Template *Template

		// Encoder to create request instance bytes from attributes
		Encoder dynamic.Encoder

		// Params of the instance; used to to create the config SHA.
		Params map[string]interface{}

		// AttributeBindings used to map the adapter output back into attributes
		AttributeBindings map[string]string

		// Language runtime to use for output expressions
		Language lang.LanguageRuntime
	}

	// InstanceStatic configuration for compiled templates. Fully resolved.
	InstanceStatic struct {
		// Name of the instance. Fully qualified.
		Name string

		// Associated template. Always resolved.
		Template *template.Info

		// parameters used to construct the instance.
		Params proto.Message

		// inferred type for the instance.
		InferredType proto.Message

		// Language runtime to use for output expressions
		Language lang.LanguageRuntime
	}

	// Rule configuration. Fully resolved.
	Rule struct {
		// Name of the rule
		Name string

		// Namespace of the rule
		Namespace string

		// Match condition
		Match string

		ActionsDynamic []*ActionDynamic

		ActionsStatic []*ActionStatic

		RequestHeaderOperations []*v1beta1.Rule_HeaderOperationTemplate

		ResponseHeaderOperations []*v1beta1.Rule_HeaderOperationTemplate

		// Language runtime to use for expressions
		Language lang.LanguageRuntime
	}

	// ActionDynamic configuration. Fully resolved.
	ActionDynamic struct {
		// Handler that this action is resolved to.
		Handler *HandlerDynamic
		// Instances that should be generated as part of invoking action.
		Instances []*InstanceDynamic
		// Name of the action (optional)
		Name string
	}

	// ActionStatic configuration. Fully resolved.
	ActionStatic struct {
		// Handler that this action is resolved to.
		Handler *HandlerStatic
		// Instances that should be generated as part of invoking action.
		Instances []*InstanceStatic
		// Name of the action (optional)
		Name string
	}

	// Template contains info about a template
	Template struct {
		// Name of the template.
		//
		// Note this is the template's resource name and not the template's internal name that adapter developer
		// uses to implement adapter service.
		Name string

		// Variety of this template
		Variety adptTmpl.TemplateVariety

		// InternalPackageDerivedName is the name of the template from adapter developer point of view.
		// The service and functions implemented by the adapter is based on this name
		// NOTE: This name derived from template proto package and not the resource name.
		InternalPackageDerivedName string

		// Template's file descriptor set.
		FileDescSet *descriptor.FileDescriptorSet

		// package name of the `Template` message
		PackageName string

		// AttributeManifest declares the output attributes for the template.
		// For attribute producing adapters, the output attributes are of the form $out.field_name.
		AttributeManifest map[string]*v1beta1.AttributeManifest_AttributeInfo
	}

	// Adapter contains info about an adapter
	Adapter struct {
		Name string

		// Adapter's file descriptor set.
		ConfigDescSet *descriptor.FileDescriptorSet

		// package name of the `Params` message
		PackageName string

		SupportedTemplates []*Template

		SessionBased bool

		Description string
	}
)

// Empty returns a new, empty configuration snapshot.
func Empty() *Snapshot {
	return &Snapshot{
		ID:                -1,
		Rules:             []*Rule{},
		MonitoringContext: context.Background(),
	}
}

// GetName gets name
func (h HandlerStatic) GetName() string {
	return h.Name
}

// AdapterName gets adapter name
func (h HandlerStatic) AdapterName() string {
	return h.Adapter.Name
}

// AdapterParams gets AdapterParams
func (h HandlerStatic) AdapterParams() interface{} {
	return h.Params
}

// ConnectionConfig returns nil for static handler
func (h HandlerStatic) ConnectionConfig() interface{} {
	return nil
}

// GetName gets name
func (i InstanceStatic) GetName() string {
	return i.Name
}

// TemplateName gets TemplateName
func (i InstanceStatic) TemplateName() string {
	return i.Template.Name
}

// TemplateParams gets TemplateParams
func (i InstanceStatic) TemplateParams() interface{} {
	return i.Params
}

// GetName gets name
func (h HandlerDynamic) GetName() string {
	return h.Name
}

// AdapterName gets adapter name
func (h HandlerDynamic) AdapterName() string {
	return h.Adapter.Name
}

// AdapterParams gets AdapterParams
func (h HandlerDynamic) AdapterParams() interface{} {
	return h.AdapterConfig
}

// ConnectionConfig gets connection config of dynamic handler
func (h HandlerDynamic) ConnectionConfig() interface{} {
	return h.Connection
}

// GetName gets name
func (i InstanceDynamic) GetName() string {
	return i.Name
}

// TemplateName gets TemplateName
func (i InstanceDynamic) TemplateName() string {
	return i.Template.Name
}

// TemplateParams gets TemplateParams
func (i InstanceDynamic) TemplateParams() interface{} {
	return i.Params
}
