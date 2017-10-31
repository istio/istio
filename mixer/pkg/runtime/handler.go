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

package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/glog"

	pbd "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/template"
)

type (
	// BuilderInfoFinder is used to find specific handlers Info for configuration.
	BuilderInfoFinder func(name string) (*adapter.Info, bool)

	// TemplateFinder finds a template by name.
	TemplateFinder interface {
		GetTemplateInfo(template string) (template.Info, bool)
	}
	// HandlerFactory builds adapter.Handler object from adapter and instances configuration.
	HandlerFactory interface {
		Build(*pb.Handler, []*pb.Instance, adapter.Env) (adapter.Handler, error)
	}

	handlerFactory struct {
		tmplRepo          TemplateFinder
		typeChecker       expr.TypeChecker
		attrDescFinder    expr.AttributeDescriptorFinder
		builderInfoFinder BuilderInfoFinder

		// protects cache
		lock           sync.RWMutex
		infrdTypsCache typeMap
	}

	// Map of instance name to inferred type (proto.Message)
	typeMap map[string]proto.Message

	templateFinder struct {
		templateInfo map[string]template.Info
	}
)

func (t *templateFinder) GetTemplateInfo(template string) (template.Info, bool) {
	i, found := t.templateInfo[template]
	return i, found
}

func newHandlerFactory(templateInfo map[string]template.Info, expr expr.TypeChecker,
	df expr.AttributeDescriptorFinder, builderInfo map[string]*adapter.Info) HandlerFactory {
	return NewHandlerFactory(&templateFinder{
		templateInfo: templateInfo,
	}, expr, df, func(name string) (*adapter.Info, bool) {
		i, found := builderInfo[name]
		return i, found
	})
}

// NewHandlerFactory instantiates a HandlerFactory, the state of the HandlerFactory is only valid for a snapshot of a configuration.
// Therefore, a new HandlerFactory should be created upon every configuration change.
func NewHandlerFactory(tmplRepo TemplateFinder, expr expr.TypeChecker, df expr.AttributeDescriptorFinder,
	builderInfoFinder BuilderInfoFinder) HandlerFactory {
	return &handlerFactory{
		tmplRepo:          tmplRepo,
		attrDescFinder:    df,
		typeChecker:       expr,
		infrdTypsCache:    make(typeMap),
		builderInfoFinder: builderInfoFinder,
	}
}

// Build instantiates a Handler object using the passed in handler and instances configuration.
func (h *handlerFactory) Build(handler *pb.Handler, instances []*pb.Instance, env adapter.Env) (adapter.Handler, error) {
	infrdTypsByTmpl, err := h.inferTypesGrpdByTmpl(instances)
	if err != nil {
		glog.Error(err.Error())
		return nil, err
	}

	// HandlerBuilder should always be present for a valid configuration (reference integrity should already be checked).
	info, _ := h.builderInfoFinder(handler.Adapter)

	bldr := info.NewBuilder()

	if bldr == nil {
		msg := fmt.Sprintf("nil HandlerBuilder instantiated for adapter '%s' in handler config '%s'", handler.Adapter, handler.Name)
		glog.Warning(msg)
		return nil, errors.New(msg)
	}

	// validate if the builder supports all the necessary interfaces
	for _, tmplName := range info.SupportedTemplates {
		// ti should be there for a valid configuration.
		ti, _ := h.tmplRepo.GetTemplateInfo(tmplName)
		if supports := ti.BuilderSupportsTemplate(bldr); !supports {
			// adapter's builder is bad since it does not support the necessary interface
			msg := fmt.Sprintf("adapter is invalid because it does not implement interface '%s'. "+
				"Therefore, it cannot support template '%s'", ti.BldrInterfaceName, tmplName)
			glog.Error(msg)
			return nil, fmt.Errorf(msg)
		}
	}

	var hndlr adapter.Handler
	hndlr, err = h.build(bldr, infrdTypsByTmpl, handler.Params, env)
	if err != nil {
		msg := fmt.Sprintf("cannot configure adapter '%s' in handler config '%s': %v", handler.Adapter, handler.Name, err)
		glog.Warning(msg)
		return nil, errors.New(msg)
	}
	// validate if the handler supports all the necessary interfaces
	for _, tmplName := range info.SupportedTemplates {
		// ti should be there for a valid configuration.
		ti, _ := h.tmplRepo.GetTemplateInfo(tmplName)
		if supports := ti.HandlerSupportsTemplate(hndlr); !supports {
			// adapter is bad since it does not support the necessary interface
			msg := fmt.Sprintf("adapter is invalid because it does not implement interface '%s'. "+
				"Therefore, it cannot support template '%s'", ti.HndlrInterfaceName, tmplName)
			glog.Error(msg)
			return nil, fmt.Errorf(msg)
		}
	}

	return hndlr, err
}

func (h *handlerFactory) build(bldr adapter.HandlerBuilder, infrdTypesByTmpl map[string]typeMap,
	adapterCnfg interface{}, env adapter.Env) (hndlr adapter.Handler, err error) {
	var ti template.Info
	var typs typeMap

	// calls into handler can panic. If that happens, we will log and return error with nil handler
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("handler panicked with '%v' when trying to configure the associated adapter."+
				" Please remove the handler or fix the configuration. %v\nti=%v\ntype=%v", r, adapterCnfg, ti, typs)
			glog.Error(msg)
			hndlr = nil
			err = errors.New(msg)
			return
		}
	}()

	for tmplName := range infrdTypesByTmpl {
		typs = infrdTypesByTmpl[tmplName]
		// ti should be there for a valid configuration.
		ti, _ = h.tmplRepo.GetTemplateInfo(tmplName)
		ti.SetType(typs, bldr)
	}
	bldr.SetAdapterConfig(adapterCnfg.(proto.Message))
	// validate and only construct if the validation passes.
	if ce := bldr.Validate(); ce != nil {
		msg := fmt.Sprintf("handler validation failed: %s", ce.Error())
		glog.Error(msg)
		hndlr = nil
		err = errors.New(msg)
		return
	}

	return bldr.Build(context.Background(), env)
}

func (h *handlerFactory) inferTypesGrpdByTmpl(instances []*pb.Instance) (map[string]typeMap, error) {
	infrdTypesByTmpl := make(map[string]typeMap)
	for _, instance := range instances {
		infrdType, err := h.inferType(instance)
		if err != nil {
			return infrdTypesByTmpl, err
		}

		if _, exists := infrdTypesByTmpl[instance.GetTemplate()]; !exists {
			infrdTypesByTmpl[instance.GetTemplate()] = make(typeMap)
		}

		infrdTypesByTmpl[instance.GetTemplate()][instance.Name] = infrdType
	}
	return infrdTypesByTmpl, nil
}

func (h *handlerFactory) inferType(instance *pb.Instance) (proto.Message, error) {

	var infrdType proto.Message
	var err error
	var found bool

	h.lock.RLock()
	infrdType, found = h.infrdTypsCache[instance.Name]
	h.lock.RUnlock()

	if found {
		return infrdType, nil
	}

	// ti should be there since the config is already validated
	tmplInfo, _ := h.tmplRepo.GetTemplateInfo(instance.GetTemplate())

	infrdType, err = tmplInfo.InferType(instance.GetParams().(proto.Message), func(expr string) (pbd.ValueType, error) {
		return h.typeChecker.EvalType(expr, h.attrDescFinder)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot infer type information from params in instance '%s': %v", instance.Name, err)
	}

	// obtain write lock
	h.lock.Lock()
	h.infrdTypsCache[instance.Name] = infrdType
	h.lock.Unlock()

	return infrdType, nil
}
