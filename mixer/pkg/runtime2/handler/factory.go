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
	"istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/evaluator"
	"istio.io/istio/mixer/pkg/log"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/template"
)

// Map of instance name to inferred type (proto.Message)
type inferredTypes map[string]proto.Message

// Factory builds adapter.Handler object from adapter and instances configuration.
type Factory interface {
	Build(*istio_mixer_v1_config.Handler, []*istio_mixer_v1_config.Instance, adapter.Env) (adapter.Handler, error)
}

type factory struct {
	snapshot *config.Snapshot

	checker expr.TypeChecker

	// protects cache
	inferredTypesLock sync.RWMutex

	// Map of instance name to inferred type (proto.Message)
	inferredTypesCache map[string]proto.Message
}

func NewFactory(snapshot *config.Snapshot) Factory {
	return &factory{
		snapshot: snapshot,
		checker:  evaluator.NewTypeChecker(),
	}
}

// Instantiate instantiates a Handler object using the passed in handler and instances configuration.
func (h *factory) Build(handlerConfig *istio_mixer_v1_config.Handler, instanceConfigs []*istio_mixer_v1_config.Instance,
	env adapter.Env) (adapter.Handler, error) {

	inferredTypesByTemplates, err := h.inferTypes(instanceConfigs)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// HandlerBuilder should always be present for a valid configuration (reference integrity should already be checked).
	info, _ := h.snapshot.AdapterInfo(handlerConfig.Adapter)

	builder := info.NewBuilder()
	if builder == nil {
		msg := fmt.Sprintf("nil HandlerBuilder instantiated for adapter '%s' in handlerConfig config '%s'", handlerConfig.Adapter, handlerConfig.Name)
		log.Warn(msg)
		return nil, errors.New(msg)
	}

	// validate if the builder supports all the necessary interfaces
	for _, tmplName := range info.SupportedTemplates {
		// TODO: !!! Is this true? Neither information is coming from config. It is not validated.
		// ti should be there for a valid configuration.
		ti, _ := h.snapshot.TemplateInfo(tmplName)

		if supports := ti.BuilderSupportsTemplate(builder); !supports {
			// adapter's builder is bad since it does not support the necessary interface
			msg := fmt.Sprintf("adapter is invalid because it does not implement interface '%s'. "+
				"Therefore, it cannot support template '%s'", ti.BldrInterfaceName, tmplName)
			log.Error(msg)
			return nil, fmt.Errorf(msg)
		}
	}

	var handler adapter.Handler
	handler, err = h.build(builder, inferredTypesByTemplates, handlerConfig.Params, env)
	if err != nil {
		msg := fmt.Sprintf("cannot configure adapter '%s' in handlerConfig config '%s': %v", handlerConfig.Adapter, handlerConfig.Name, err)
		log.Warn(msg)
		return nil, errors.New(msg)
	}
	// validate if the handlerConfig supports all the necessary interfaces
	for _, tmplName := range info.SupportedTemplates {
		// ti should be there for a valid configuration.
		ti, _ := h.snapshot.TemplateInfo(tmplName)

		if supports := ti.HandlerSupportsTemplate(handler); !supports {
			// adapter is bad since it does not support the necessary interface
			msg := fmt.Sprintf("adapter is invalid because it does not implement interface '%s'. "+
				"Therefore, it cannot support template '%s'", ti.HndlrInterfaceName, tmplName)
			log.Error(msg)
			return nil, fmt.Errorf(msg)
		}
	}

	return handler, err
}

func (h *factory) build(builder adapter.HandlerBuilder, inferredTypes map[string]inferredTypes,
	adapterConfig interface{}, env adapter.Env) (handler adapter.Handler, err error) {
	var ti template.Info
	var types inferredTypes

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
		ti, _ = h.snapshot.TemplateInfo(tmplName)
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

	return builder.Build(context.Background(), env)
}

func (h *factory) inferTypes(instanceConfigs []*istio_mixer_v1_config.Instance) (map[string]inferredTypes, error) {

	typesByTemplate := make(map[string]inferredTypes)
	for _, instanceConfig := range instanceConfigs {

		inferredType, err := h.inferType(instanceConfig)
		if err != nil {
			return nil, err
		}

		if _, exists := typesByTemplate[instanceConfig.GetTemplate()]; !exists {
			typesByTemplate[instanceConfig.GetTemplate()] = make(inferredTypes)
		}

		typesByTemplate[instanceConfig.GetTemplate()][instanceConfig.Name] = inferredType
	}
	return typesByTemplate, nil
}

func (h *factory) inferType(instanceConfig *istio_mixer_v1_config.Instance) (proto.Message, error) {

	var inferredType proto.Message
	var err error
	var found bool

	h.inferredTypesLock.RLock()
	inferredType, found = h.inferredTypesCache[instanceConfig.Name]
	h.inferredTypesLock.RUnlock()

	if found {
		return inferredType, nil
	}

	// t should be there since the config is already validated
	template, _ := h.snapshot.TemplateInfo(instanceConfig.GetTemplate())

	inferredType, err = template.InferType(instanceConfig.GetParams().(proto.Message), func(expr string) (istio_mixer_v1_config_descriptor.ValueType, error) {
		return h.checker.EvalType(expr, h.snapshot.Attributes())
	})
	if err != nil {
		return nil, fmt.Errorf("cannot infer type information from params in instanceConfig '%s': %v", instanceConfig.Name, err)
	}

	// obtain write lock
	h.inferredTypesLock.Lock()
	h.inferredTypesCache[instanceConfig.Name] = inferredType
	h.inferredTypesLock.Unlock()

	return inferredType, nil
}
