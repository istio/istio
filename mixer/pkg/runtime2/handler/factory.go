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
	"errors"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/evaluator"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
)

// Map of instance name to inferred type (proto.Message)
type inferredTypesMap map[string]proto.Message

// factory is used to instantiate handlers.
type factory struct {
	snapshot *config.Snapshot

	checker expr.TypeChecker

	// Map of instance name to inferred type (proto.Message)
	inferredTypesCache map[string]proto.Message
}

func newFactory(snapshot *config.Snapshot) *factory {
	return &factory{
		snapshot: snapshot,
		checker:  evaluator.NewTypeChecker(),

		inferredTypesCache: make(map[string]proto.Message),
	}
}

// build instantiates a handler object using the passed in handler and instances configuration.
func (f *factory) build(
	handler *config.Handler,
	instances []*config.Instance,
	env adapter.Env) (adapter.Handler, error) {

	inferredTypesByTemplates, err := f.inferTypes(instances)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// HandlerBuilder should always be present for a valid configuration (reference integrity should already be checked).
	info := handler.Adapter

	builder := info.NewBuilder()
	if builder == nil {
		return nil, errors.New("nil HandlerBuilder")
	}

	// validate if the builder supports all the necessary interfaces
	for _, tmplName := range info.SupportedTemplates {

		ti, found := f.snapshot.Templates[tmplName]
		if !found {
			// TODO (Issue #2512): This log is unnecessarily spammy. We should test for this during startup
			// and log it once.
			// One of the templates that is supported by the adapter was not found. We should log and simply
			// move on.
			log.Infof("Ignoring unrecognized template, supported by adapter: adapter='%s', template='%s'",
				handler.Adapter.NewBuilder, tmplName)
			continue
		}

		if supports := ti.BuilderSupportsTemplate(builder); !supports {
			// TODO (Issue #2512): This will cause spammy logging at the call site.
			// We should test for this during startup and log it once.
			return nil, fmt.Errorf("adapter does not actually support template: template='%s', interface='%s'", tmplName, ti.BldrInterfaceName)
		}
	}

	instantiatedAdapter, err := f.buildHandler(builder, inferredTypesByTemplates, handler.Params, env)
	if err != nil {
		return nil, fmt.Errorf("adapter instantiation error: %v", err)
	}

	// validate if the handlerConfig supports all the necessary interfaces
	for _, tmplName := range info.SupportedTemplates {
		// ti should be there for a valid configuration.
		ti, found := f.snapshot.Templates[tmplName]
		if !found {
			// This is similar to the condition check in the previous loop. That already does logging, so
			// simply skip.
			continue
		}

		if supports := ti.HandlerSupportsTemplate(instantiatedAdapter); !supports {
			closeHandler(instantiatedAdapter)

			// adapter is bad since it does not support the necessary interface
			return nil, fmt.Errorf("builder for adapter does not actually support template: template='%s', interface='%s'",
				tmplName, ti.HndlrInterfaceName)
		}
	}

	return instantiatedAdapter, err
}

func closeHandler(h adapter.Handler) {
	// calls into handler can panic. If that happens, we will log and return error with nil handler
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Adapter panicked when trying to close: '%v' ", r)
			return
		}
	}()

	if err := h.Close(); err != nil {
		log.Errorf("Adapter returned error during closing: %v", err)
	}
}

func (f *factory) buildHandler(
	builder adapter.HandlerBuilder,
	inferredTypes map[string]inferredTypesMap,
	adapterConfig interface{},
	env adapter.Env) (handler adapter.Handler, err error) {
	var ti *template.Info
	var types inferredTypesMap

	// calls into handler can panic. If that happens, we will log and return error with nil handler
	defer func() {
		if r := recover(); r != nil {
			handler = nil
			err = fmt.Errorf("handler panicked during configuration: '%v', types:='%s'", r, types)
			return
		}
	}()

	for tmplName := range inferredTypes {
		types = inferredTypes[tmplName]
		// ti should be there for a valid configuration.
		ti, _ = f.snapshot.Templates[tmplName]
		if ti.SetType != nil { // for case like APA template that does not have SetType
			ti.SetType(types, builder)
		}
	}
	builder.SetAdapterConfig(adapterConfig.(proto.Message))
	// validate and only construct if the validation passes.
	if ce := builder.Validate(); ce != nil {
		handler = nil
		err = fmt.Errorf("handler validation failed: '%v'", ce)
		return
	}

	return builder.Build(context.Background(), env)
}

func (f *factory) inferTypes(instances []*config.Instance) (map[string]inferredTypesMap, error) {

	typesByTemplate := make(map[string]inferredTypesMap)
	for _, instance := range instances {

		inferredType, err := f.inferType(instance)
		if err != nil {
			return nil, err
		}

		if _, exists := typesByTemplate[instance.Template.Name]; !exists {
			typesByTemplate[instance.Template.Name] = make(inferredTypesMap)
		}

		typesByTemplate[instance.Template.Name][instance.Name] = inferredType
	}
	return typesByTemplate, nil
}

func (f *factory) inferType(instance *config.Instance) (proto.Message, error) {

	var inferredType proto.Message
	var err error
	var found bool

	inferredType, found = f.inferredTypesCache[instance.Name]

	if found {
		return inferredType, nil
	}

	// t should be there since the config is already validated
	t := instance.Template

	inferredType, err = t.InferType(instance.Params, func(expr string) (istio_mixer_v1_config_descriptor.ValueType, error) {
		return f.checker.EvalType(expr, f.snapshot.Attributes)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot infer type information from params in instanceConfig '%s': %v", instance.Name, err)
	}

	f.inferredTypesCache[instance.Name] = inferredType

	return inferredType, nil
}
