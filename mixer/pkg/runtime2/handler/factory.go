// Copyright 2017 Istio Authors
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
	"sync"

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

type factory struct {
	snapshot *config.Snapshot

	checker expr.TypeChecker

	// protects cache
	inferredTypesLock sync.RWMutex

	// Map of instance name to inferred type (proto.Message)
	inferredTypesCache map[string]proto.Message

	env adapter.Env
}

func newFactory(snapshot *config.Snapshot, env adapter.Env) *factory {
	return &factory{
		snapshot: snapshot,
		checker:  evaluator.NewTypeChecker(),
		env:      env,

		inferredTypesCache: make(map[string]proto.Message),
	}
}

// build instantiates a handler object using the passed in handler and instances configuration.
func (f *factory) build(
	handler *config.Handler,
	instances []*config.Instance) (adapter.Handler, error) {

	inferredTypesByTemplates, err := f.inferTypes(instances)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// HandlerBuilder should always be present for a valid configuration (reference integrity should already be checked).
	info := handler.Adapter

	builder := info.NewBuilder()
	if builder == nil {
		msg := fmt.Sprintf("nil HandlerBuilder instantiated for adapter '%s' in handlerConfig config '%s'", handler.Adapter.Name, handler.Name)
		log.Warn(msg)
		return nil, errors.New(msg)
	}

	// validate if the builder supports all the necessary interfaces
	for _, tmplName := range info.SupportedTemplates {
		// TODO: !!! Is this true? Neither information is coming from config. It is not validated.
		// ti should be there for a valid configuration.
		ti, _ := f.snapshot.Templates[tmplName]

		if supports := ti.BuilderSupportsTemplate(builder); !supports {
			// adapter's builder is bad since it does not support the necessary interface
			msg := fmt.Sprintf("adapter is invalid because it does not implement interface '%s'. "+
				"Therefore, it cannot support template '%s'", ti.BldrInterfaceName, tmplName)
			log.Error(msg)
			return nil, fmt.Errorf(msg)
		}
	}

	instantiatedAdapter, err := f.buildHandler(builder, inferredTypesByTemplates, handler.Params)
	if err != nil {
		msg := fmt.Sprintf("cannot configure adapter '%s' in handlerConfig config '%s': %v", handler.Adapter.Name, handler.Name, err)
		log.Warn(msg)
		return nil, errors.New(msg)
	}
	// validate if the handlerConfig supports all the necessary interfaces
	for _, tmplName := range info.SupportedTemplates {
		// ti should be there for a valid configuration.
		ti, _ := f.snapshot.Templates[tmplName]

		// TODO: !!! The adapter is instantiated. Should we simply drop it on the floor?
		if supports := ti.HandlerSupportsTemplate(instantiatedAdapter); !supports {
			// adapter is bad since it does not support the necessary interface
			msg := fmt.Sprintf("adapter is invalid because it does not implement interface '%s'. "+
				"Therefore, it cannot support template '%s'", ti.HndlrInterfaceName, tmplName)
			log.Error(msg)
			return nil, fmt.Errorf(msg)
		}
	}

	return instantiatedAdapter, err
}

func (f *factory) buildHandler(builder adapter.HandlerBuilder, inferredTypes map[string]inferredTypesMap,
	adapterConfig interface{}) (handler adapter.Handler, err error) {
	var ti *template.Info
	var types inferredTypesMap

	// calls into handler can panic. If that happens, we will log and return error with nil handler
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("handler panicked with '%v' when trying to configure the associated adapter."+
				" Please remove the handler or fix the configuration. %v\nti=%v\ntype=%v", r, adapterConfig, ti, types)
			log.Error(msg)
			handler = nil
			err = errors.New(msg)
			return
		}
	}()

	for tmplName := range inferredTypes {
		types = inferredTypes[tmplName]
		// ti should be there for a valid configuration.
		ti, _ = f.snapshot.Templates[tmplName]
		ti.SetType(types, builder)
	}
	builder.SetAdapterConfig(adapterConfig.(proto.Message))
	// validate and only construct if the validation passes.
	if ce := builder.Validate(); ce != nil {
		msg := fmt.Sprintf("handler validation failed: %s", ce.Error())
		log.Error(msg)
		handler = nil
		err = errors.New(msg)
		return
	}

	return builder.Build(context.Background(), f.env)
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

	f.inferredTypesLock.RLock()
	inferredType, found = f.inferredTypesCache[instance.Name]
	f.inferredTypesLock.RUnlock()

	if found {
		return inferredType, nil
	}

	// t should be there since the config is already validated
	template := instance.Template

	inferredType, err = template.InferType(instance.Params, func(expr string) (istio_mixer_v1_config_descriptor.ValueType, error) {
		return f.checker.EvalType(expr, f.snapshot.Attributes)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot infer type information from params in instanceConfig '%s': %v", instance.Name, err)
	}

	// obtain write lock
	f.inferredTypesLock.Lock()
	f.inferredTypesCache[instance.Name] = inferredType
	f.inferredTypesLock.Unlock()

	return inferredType, nil
}
