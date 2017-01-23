// Copyright 2017 Google Inc.
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
	"time"

	"google.golang.org/genproto/googleapis/rpc/code"

	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"

	dpb "istio.io/api/mixer/v1/config/descriptor"
)

type (
	quotaManager struct{}

	quotaWrapper struct {
		aspect      adapter.QuotaAspect
		descriptors []dpb.QuotaDescriptor
		inputs      map[string]string
	}
)

// NewQuotaManager returns an instance of the Quota aspect manager.
func NewQuotaManager() Manager {
	return quotaManager{}
}

// NewAspect creates a quota aspect.
func (m quotaManager) NewAspect(c *config.Combined, a adapter.Builder, env adapter.Env) (Wrapper, error) {
	// TODO: get this from config
	desc := []dpb.QuotaDescriptor{}
	defs := make(map[string]*adapter.QuotaDefinition)
	for _, d := range desc {
		defs[d.Name] = &adapter.QuotaDefinition{
			MaxAmount: d.MaxAmount,
			Window:    time.Duration(d.ExpirationSeconds) * time.Second,
		}
	}

	aspect, err := a.(adapter.QuotaBuilder).NewQuota(env, c.Builder.Params.(adapter.AspectConfig), defs)
	if err != nil {
		return nil, err
	}

	return &quotaWrapper{
		descriptors: desc,
		inputs:      c.Aspect.GetInputs(),
		aspect:      aspect,
	}, nil
}

func (quotaManager) Kind() string                                                   { return "istio/quota" }
func (quotaManager) DefaultConfig() adapter.AspectConfig                            { return &aconfig.QuotaParams{} }
func (quotaManager) ValidateConfig(adapter.AspectConfig) (ce *adapter.ConfigErrors) { return }

func (w *quotaWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error) {
	// labels holds the generated attributes from mapper
	labels := make(map[string]interface{})
	for attr, expr := range w.inputs {
		if val, err := mapper.Eval(expr, attrs); err == nil {
			labels[attr] = val
		}
	}

	for _, d := range w.descriptors {
		amount, ok := attrs.Int64(d.AmountAttribute)
		if !ok {
			// TODO: bail out!
		}

		dedupID := ""

		args := adapter.QuotaArgs{
			Name:            d.Name,
			Labels:          make(map[string]interface{}),
			QuotaAmount:     amount,
			DeduplicationID: dedupID,
		}

		for _, a := range d.Attributes {
			if val, ok := labels[a]; ok {
				args.Labels[a] = val
				continue
			}
			if val, found := attribute.Value(attrs, a); found {
				args.Labels[a] = val
			}
		}

		// TODO: handle return value!
		_, _ = w.aspect.Alloc(args)
	}

	return &Output{Code: code.Code_OK}, nil
}

func (w *quotaWrapper) Close() error {
	return w.aspect.Close()
}
