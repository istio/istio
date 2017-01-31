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
	"strconv"
	"sync/atomic"
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
	quotaManager struct {
		dedupCounter int64
	}

	quotaWrapper struct {
		manager     *quotaManager
		aspect      adapter.QuotaAspect
		descriptors []dpb.QuotaDescriptor
		inputs      map[string]string
	}
)

// NewQuotaManager returns an instance of the Quota aspect manager.
func NewQuotaManager() Manager {
	return &quotaManager{}
}

// NewAspect creates a quota aspect.
func (m *quotaManager) NewAspect(c *config.Combined, a adapter.Builder, env adapter.Env) (Wrapper, error) {
	// TODO: get this from config
	desc := []dpb.QuotaDescriptor{
		{
			Name:              "RequestCount",
			MaxAmount:         5,
			ExpirationSeconds: 1,
		},
	}

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
		manager:     m,
		descriptors: desc,
		inputs:      c.Aspect.GetInputs(),
		aspect:      aspect,
	}, nil
}

func (*quotaManager) Kind() string                                                   { return "istio/quota" }
func (*quotaManager) DefaultConfig() adapter.AspectConfig                            { return &aconfig.QuotaParams{} }
func (*quotaManager) ValidateConfig(adapter.AspectConfig) (ce *adapter.ConfigErrors) { return }

func (w *quotaWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error) {
	// labels holds the generated attributes from mapper
	labels := make(map[string]interface{})
	for attr, expr := range w.inputs {
		if val, err := mapper.Eval(expr, attrs); err == nil {
			labels[attr] = val
		}
	}

	// TODO: for now we don't support dedup semantics
	dedupID := strconv.FormatInt(atomic.AddInt64(&w.manager.dedupCounter, 1), 16)

	for i, d := range w.descriptors {
		qa := prepQuotaArgs(attrs, &d, dedupID, labels)

		// TODO: for now we only support Alloc semantics, no AllocBestEffort or ReleaseBestEffort
		if amount, err := w.aspect.Alloc(qa); err != nil || amount <= 0 {
			// something went wrong, return any allocated quota
			for j := i; j >= 0; j-- {
				qa := prepQuotaArgs(attrs, &w.descriptors[j], dedupID, labels)
				_, _ = w.aspect.ReleaseBestEffort(qa)
			}

			if err != nil {
				return &Output{Code: code.Code_INTERNAL}, err
			}

			return &Output{Code: code.Code_RESOURCE_EXHAUSTED}, nil
		}
	}

	return &Output{Code: code.Code_OK}, nil
}

func prepQuotaArgs(attrs attribute.Bag, d *dpb.QuotaDescriptor,
	dedupID string, labels map[string]interface{}) adapter.QuotaArgs {
	amount, ok := attrs.Int64(d.AmountAttribute)
	if !ok {
		// TODO: for now, assume no one passed in the amount
		amount = 1
	}

	qa := adapter.QuotaArgs{
		Name:            d.Name,
		Labels:          make(map[string]interface{}),
		QuotaAmount:     amount,
		DeduplicationID: dedupID,
	}

	for _, a := range d.Attributes {
		if val, ok := labels[a]; ok {
			qa.Labels[a] = val
			continue
		}
		if val, found := attribute.Value(attrs, a); found {
			qa.Labels[a] = val
		}
	}

	return qa
}

func (w *quotaWrapper) Close() error {
	return w.aspect.Close()
}
