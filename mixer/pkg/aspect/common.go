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

package aspect

import (
	"fmt"

	multierror "github.com/hashicorp/go-multierror"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/attribute"
	cfg "istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
)

// FromHandler creates a CreateAspectFunc from the provided handler instance.
func FromHandler(handler adapter.Handler) CreateAspectFunc {
	return func(adapter.Env, adapter.Config, ...interface{}) (adapter.Aspect, error) {
		return handler, nil
	}
}

// FromBuilder creates a CreateAspectFunc from the provided builder instance, dispatching to New*Aspect methods based
// on the kind parameter.
func FromBuilder(builder adapter.Builder, kind cfg.Kind) (CreateAspectFunc, error) {
	switch kind {
	case cfg.AccessLogsKind:
		b, ok := builder.(adapter.AccessLogsBuilder)
		if !ok {
			return nil, fmt.Errorf("invalid builder - kind AccessLogsKind expected builder implementing AccessLogsBuilder, got builder: %v", builder)
		}
		return func(env adapter.Env, c adapter.Config, _ ...interface{}) (adapter.Aspect, error) {
			return b.NewAccessLogsAspect(env, c)
		}, nil
	case cfg.ApplicationLogsKind:
		b, ok := builder.(adapter.ApplicationLogsBuilder)
		if !ok {
			return nil, fmt.Errorf("invalid builder - kind ApplicationLogsKind expected builder implementing ApplicationLogsBuilder, got builder: %v", builder)
		}
		return func(env adapter.Env, c adapter.Config, _ ...interface{}) (adapter.Aspect, error) {
			return b.NewApplicationLogsAspect(env, c)
		}, nil
	case cfg.AttributesKind:
		b, ok := builder.(adapter.AttributesGeneratorBuilder)
		if !ok {
			return nil, fmt.Errorf("invalid builder - kind AttributesKind expected builder implementing AttributesGeneratorBuilder, got builder: %v", builder)
		}
		return func(env adapter.Env, c adapter.Config, _ ...interface{}) (adapter.Aspect, error) {
			return b.BuildAttributesGenerator(env, c)
		}, nil
	case cfg.DenialsKind:
		b, ok := builder.(adapter.DenialsBuilder)
		if !ok {
			return nil, fmt.Errorf("invalid builder - kind DenialsKind expected builder implementing DenialsBuilder, got builder: %v", builder)
		}
		return func(env adapter.Env, c adapter.Config, _ ...interface{}) (adapter.Aspect, error) {
			return b.NewDenialsAspect(env, c)
		}, nil
	case cfg.ListsKind:
		b, ok := builder.(adapter.ListsBuilder)
		if !ok {
			return nil, fmt.Errorf("invalid builder - kind ListsKind expected builder implementing ListsBuilder, got builder: %v", builder)
		}
		return func(env adapter.Env, c adapter.Config, _ ...interface{}) (adapter.Aspect, error) {
			return b.NewListsAspect(env, c)
		}, nil
	case cfg.MetricsKind:
		b, ok := builder.(adapter.MetricsBuilder)
		if !ok {
			return nil, fmt.Errorf("invalid builder - kind MetricsKind expected builder implementing MetricsBuilder, got builder: %v", builder)
		}
		return func(env adapter.Env, c adapter.Config, cfg ...interface{}) (adapter.Aspect, error) {
			if len(cfg) != 1 {
				return nil, fmt.Errorf("metric builders must have configuration args")
			}
			metrics, ok := cfg[0].(map[string]*adapter.MetricDefinition)
			if !ok {
				return nil, fmt.Errorf("arg to metrics builder must be a map[string]*adapter.MetricDefinition, got: %#v", cfg[0])
			}
			return b.NewMetricsAspect(env, c, metrics)
		}, nil
	case cfg.QuotasKind:
		b, ok := builder.(adapter.QuotasBuilder)
		if !ok {
			return nil, fmt.Errorf("invalid builder - kind QuotasKind expected builder implementing QuotasBuilder, go buildert: %v", builder)
		}
		return func(env adapter.Env, c adapter.Config, cfg ...interface{}) (adapter.Aspect, error) {
			if len(cfg) != 1 {
				return nil, fmt.Errorf("quota builders must have configuration args")
			}
			quotas, ok := cfg[0].(map[string]*adapter.QuotaDefinition)
			if !ok {
				return nil, fmt.Errorf("arg to quota builder must be a map[string]*adapter.QuotaDefinition, got: %#v", cfg[0])
			}
			return b.NewQuotasAspect(env, c, quotas)
		}, nil
	default:
		return nil, fmt.Errorf("invalid kind %v", kind)
	}
}

func evalAll(expressions map[string]string, attrs attribute.Bag, eval expr.Evaluator) (map[string]interface{}, error) {
	result := &multierror.Error{}
	labels := make(map[string]interface{}, len(expressions))
	for label, texpr := range expressions {
		val, err := eval.Eval(texpr, attrs)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to construct value for label '%s': %v", label, err))
			continue
		}
		labels[label] = val
	}
	return labels, result.ErrorOrNil()
}

func validateLabels(ceField string, labels map[string]string, labelDescs map[string]dpb.ValueType, v expr.TypeChecker, df expr.AttributeDescriptorFinder) (
	ce *adapter.ConfigErrors) {

	if len(labels) != len(labelDescs) {
		ce = ce.Appendf(ceField, "wrong dimensions: descriptor expects %d labels, found %d labels", len(labelDescs), len(labels))
	}
	for name, exp := range labels {
		if labelType, found := labelDescs[name]; !found {
			ce = ce.Appendf(ceField, "wrong dimensions: extra label named %s", name)
		} else if err := v.AssertType(exp, df, labelType); err != nil {
			ce = ce.Appendf(ceField, "error type checking label '%s': %v", name, err)
		}
	}
	return
}

func validateTemplateExpressions(ceField string, expressions map[string]string, tc expr.TypeChecker, df expr.AttributeDescriptorFinder) (
	ce *adapter.ConfigErrors) {

	// We can't do type assertions since we don't know what each template param needs to resolve to, but we can
	// make sure they're syntactically correct and we have the attributes they need available in the system.
	for name, exp := range expressions {
		if _, err := tc.EvalType(exp, df); err != nil {
			ce = ce.Appendf(ceField, "failed to parse expression '%s': %v", name, err)
		}
	}
	return
}
