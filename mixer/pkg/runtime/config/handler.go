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
	"fmt"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/safecall"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/log"
)

// inferredTypesMap represents map of instance name to inferred type proto messages
type inferredTypesMap map[string]proto.Message

// ValidateBuilder constructs and validates a builder.
func ValidateBuilder(
	handler *HandlerStatic,
	instances []*InstanceStatic, templates map[string]*template.Info) (hb adapter.HandlerBuilder, err error) {

	// Do not assign the error to err directly, as this would overwrite the err returned by the inner function.
	panicErr := safecall.Execute("factory.build", func() {
		inferredTypesByTemplates := groupInferTypesByTmpl(instances)

		// Adapter should always be present for a valid configuration (reference integrity should already be checked).
		info := handler.Adapter

		hb = info.NewBuilder()
		// validate and only construct if the validation passes.
		if err = validateBuilder(hb, templates, inferredTypesByTemplates, handler); err != nil {
			err = fmt.Errorf("adapter validation failed : %v", err)
			hb = nil
			return
		}
	})

	if panicErr != nil {
		err = panicErr
		hb = nil
		return
	}

	return
}

// BuildHandler instantiates a handler object using the passed in handler and instances configuration.
func BuildHandler(
	handler *HandlerStatic,
	instances []*InstanceStatic,
	env adapter.Env, templates map[string]*template.Info) (h adapter.Handler, err error) {

	var builder adapter.HandlerBuilder
	// Do not assign the error to err directly, as this would overwrite the err returned by the inner function.
	panicErr := safecall.Execute("factory.build", func() {
		builder, err = ValidateBuilder(handler, instances, templates)
		if err != nil {
			h = nil
			err = fmt.Errorf("adapter builder validation failed: %v", err)
			return
		}

		h, err = buildHandler(builder, env)
		if err != nil {
			h = nil
			err = fmt.Errorf("adapter instantiation error: %v", err)
			return
		}

		// validate if the handlerConfig supports all the necessary interfaces
		for _, tmplName := range handler.Adapter.SupportedTemplates {
			// ti should be there for a valid configuration.
			ti, found := templates[tmplName]
			if !found {
				// This is similar to the condition check in the previous loop. That already does logging, so
				// simply skip.
				continue
			}

			if supports := ti.HandlerSupportsTemplate(h); !supports {
				if err = h.Close(); err != nil {
					h = nil
					// log this error, but return the one below. That is likely to be the more important one.
					log.Errorf("error during adapter close: '%v'", err)
					return
				}

				h = nil
				// adapter is bad since it does not support the necessary interface
				err = fmt.Errorf("builder for adapter does not actually support template: template='%s', interface='%s'",
					tmplName, ti.HndlrInterfaceName)
				return
			}
		}
	})

	if panicErr != nil {
		h = nil
		err = panicErr
	}

	return
}

func buildHandler(builder adapter.HandlerBuilder, env adapter.Env) (handler adapter.Handler, err error) {
	return builder.Build(context.Background(), env)
}

func groupInferTypesByTmpl(instances []*InstanceStatic) map[string]inferredTypesMap {
	typesByTemplate := make(map[string]inferredTypesMap)
	for _, instance := range instances {
		if _, exists := typesByTemplate[instance.Template.Name]; !exists {
			typesByTemplate[instance.Template.Name] = make(inferredTypesMap)
		}

		typesByTemplate[instance.Template.Name][instance.Name] = instance.InferredType
	}
	return typesByTemplate
}

func validateBuilder(
	builder adapter.HandlerBuilder,
	templates map[string]*template.Info,
	inferredTypes map[string]inferredTypesMap,
	handler *HandlerStatic) (err error) {
	if builder == nil {
		err = fmt.Errorf("nil builder from adapter: adapter='%s'", handler.Adapter.Name)
		return
	}

	// validate if the builder supports all the necessary interfaces
	for _, tmplName := range handler.Adapter.SupportedTemplates {
		ti, found := templates[tmplName]
		if !found {
			// TODO (Issue #2512): This log is unnecessarily spammy. We should test for this during startup
			// and log it once.
			// One of the templates that is supported by the adapter was not found. We should log and simply
			// move on.
			log.Infof("Ignoring unrecognized template, supported by adapter: adapter='%s', template='%s'",
				handler.Adapter.Name, tmplName)
			continue
		}

		if supports := ti.BuilderSupportsTemplate(builder); !supports {
			err = fmt.Errorf("adapter does not actually support template: template='%s', interface='%s'", tmplName, ti.BldrInterfaceName)
			return
		}
	}

	for tmplName := range inferredTypes {
		types := inferredTypes[tmplName]
		// ti should be there since inferred types are created only when instance references a valid template.
		ti := templates[tmplName]
		if ti.SetType != nil { // for case like APA template that does not have SetType
			ti.SetType(types, builder)
		}
	}

	builder.SetAdapterConfig(handler.Params)

	var ce *adapter.ConfigErrors
	if ce = builder.Validate(); ce != nil {
		err = fmt.Errorf("builder validation failed: '%v'", ce)
		return
	}
	return
}
