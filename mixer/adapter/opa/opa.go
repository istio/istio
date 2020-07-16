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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/opa/config/config.proto -x "-n opa -t authorization -d example"

package opa // import "istio.io/istio/mixer/adapter/opa"

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"

	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/adapter/opa/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/authorization"
)

type (
	builder struct {
		adapterConfig *config.Params
		configErrors  []error
		compiler      *ast.Compiler
	}

	handler struct {
		checkMethod    string
		compiler       *ast.Compiler
		logger         adapter.Logger
		hasConfigError bool
		failClose      bool
	}
)

const (
	policyFileNamePrefix = "opa_policy"
)

///////////////// Configuration Methods ///////////////

func (b *builder) SetAuthorizationTypes(types map[string]*authorization.Type) {}

func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adapterConfig = cfg.(*config.Params)
}

// To support fail close, Validate will not append errors to ce but will be appended to the configErrors.
// HandleAuthorization handle request based on configErrors and failClose option
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if len(b.adapterConfig.CheckMethod) == 0 {
		msg := "check method is not configured"
		if b.adapterConfig.FailClose {
			b.configErrors = append(b.configErrors, fmt.Errorf(msg))
		} else {
			ce = ce.Appendf("CheckMethod", msg)
		}
	}

	parsedModuleCount := 0
	moduleParseErrorCount := 0
	modules := map[string]*ast.Module{}
	for index, policy := range b.adapterConfig.Policy {
		filename := fmt.Sprintf("%v.%v", policyFileNamePrefix, index)
		parsed, err := ast.ParseModule(filename, policy)
		if err != nil {
			if b.adapterConfig.FailClose {
				b.configErrors = append(b.configErrors, err)
			} else {
				ce = ce.Append("Policy", err)
			}
			moduleParseErrorCount++
		} else {
			modules[filename] = parsed
			parsedModuleCount++
		}
	}

	if moduleParseErrorCount > 0 {
		return
	}

	if parsedModuleCount == 0 {
		msg := "policies are not configured"
		if b.adapterConfig.FailClose {
			b.configErrors = append(b.configErrors, fmt.Errorf(msg))
		} else {
			ce = ce.Appendf("Policy", msg)
		}
		return
	}

	compiler := ast.NewCompiler()
	compiler.Compile(modules)
	if compiler.Failed() {
		for _, err := range compiler.Errors {
			if b.adapterConfig.FailClose {
				b.configErrors = append(b.configErrors, fmt.Errorf("%v", err.Error()))
			} else {
				ce = ce.Appendf("Policy", err.Error())
			}
		}
	} else {
		b.compiler = compiler
	}

	return
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	for _, err := range b.configErrors {
		_ = env.Logger().Errorf("%v", err)
	}

	return &handler{
		compiler:       b.compiler,
		checkMethod:    b.adapterConfig.CheckMethod,
		failClose:      b.adapterConfig.FailClose,
		logger:         env.Logger(),
		hasConfigError: len(b.configErrors) > 0,
	}, nil
}

////////////////// Runtime Methods //////////////////////////

func convertActionObjectToMap(action *authorization.Action) map[string]interface{} {
	result := map[string]interface{}{}
	if len(action.Namespace) > 0 {
		result["namespace"] = action.Namespace
	}
	if len(action.Service) > 0 {
		result["service"] = action.Service
	}
	if len(action.Method) > 0 {
		result["method"] = action.Method
	}
	if len(action.Path) > 0 {
		result["path"] = action.Path
	}

	if action.Properties != nil {
		properties := map[string]interface{}{}
		count := 0
		for key, val := range action.Properties {
			properties[key] = val
			count++
		}
		if count > 0 {
			result["properties"] = properties
		}
	}

	return result
}

func convertSubjectObjectToMap(subject *authorization.Subject) map[string]interface{} {
	result := map[string]interface{}{}
	if len(subject.User) > 0 {
		result["user"] = subject.User
	}

	// TODO(jaebong): repeated string is not supported yet.
	if len(subject.Groups) > 0 {
		result["groups"] = []interface{}{subject.Groups}
	}

	if subject.Properties != nil {
		properties := map[string]interface{}{}
		count := 0
		for key, val := range subject.Properties {
			properties[key] = val
			count++
		}
		if count > 0 {
			result["properties"] = properties
		}
	}

	return result
}

func (h *handler) handleFailClose(err error) (adapter.CheckResult, error) {
	retStatus := status.OK

	if h.failClose {
		retStatus = status.WithPermissionDenied(err.Error())
	}

	return adapter.CheckResult{
		Status: retStatus,
	}, nil
}

func (h *handler) HandleAuthorization(context context.Context, instance *authorization.Instance) (adapter.CheckResult, error) {
	// Handle configuration error
	if h.hasConfigError {
		return h.handleFailClose(fmt.Errorf("opa: request was rejected"))
	}

	// eval rego policy scripts
	rs, err := rego.New(
		rego.Compiler(h.compiler),
		rego.Query(h.checkMethod),
		rego.Input(map[string]interface{}{
			"action":  convertActionObjectToMap(instance.Action),
			"subject": convertSubjectObjectToMap(instance.Subject),
		}),
	).Eval(context)

	// Handle errors from OPA engine, policy scripts
	if err != nil {
		return h.handleFailClose(fmt.Errorf("opa: request was rejected. err: %v", err))
	}

	if len(rs) != 1 {
		return h.handleFailClose(fmt.Errorf("opa: request was rejected"))
	}

	result, ok := rs[0].Expressions[0].Value.(bool)
	if !ok {
		return h.handleFailClose(fmt.Errorf("opa: request was rejected"))
	}

	// rejected by policy scripts
	if !result {
		return adapter.CheckResult{
			Status: status.WithPermissionDenied("opa: request was rejected"),
		}, nil
	}

	// Accepted
	return adapter.CheckResult{Status: status.OK}, nil
}

func (h *handler) Close() error {
	return nil
}

////////////////// Bootstrap //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("opa")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}
