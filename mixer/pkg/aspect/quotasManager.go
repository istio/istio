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

	ptypes "github.com/gogo/protobuf/types"
	"google.golang.org/genproto/googleapis/rpc/code"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
)

type (
	quotasManager struct {
		dedupCounter int64
	}

	quotasWrapper struct {
		manager     *quotasManager
		aspect      adapter.QuotasAspect
		descriptors []dpb.QuotaDescriptor
		inputs      map[string]string
	}
)

// NewQuotasManager returns a manager for the quotas aspect.
func NewQuotasManager() Manager {
	return &quotasManager{}
}

// NewAspect creates a quota aspect.
func (m *quotasManager) NewAspect(c *config.Combined, a adapter.Builder, env adapter.Env) (Wrapper, error) {
	// TODO: get this from config
	desc := []dpb.QuotaDescriptor{
		{
			Name:       "RequestCount",
			MaxAmount:  5,
			Expiration: &ptypes.Duration{Seconds: 1},
		},
	}

	defs := make(map[string]*adapter.QuotaDefinition)
	for _, d := range desc {
		dur, _ := ptypes.DurationFromProto(d.Expiration)

		defs[d.Name] = &adapter.QuotaDefinition{
			MaxAmount: d.MaxAmount,
			Window:    dur,
		}
	}

	aspect, err := a.(adapter.QuotasBuilder).NewQuotasAspect(env, c.Builder.Params.(adapter.AspectConfig), defs)
	if err != nil {
		return nil, err
	}

	return &quotasWrapper{
		manager:     m,
		descriptors: desc,
		inputs:      c.Aspect.GetInputs(),
		aspect:      aspect,
	}, nil
}

func (*quotasManager) Kind() string                                                   { return QuotaKind }
func (*quotasManager) DefaultConfig() adapter.AspectConfig                            { return &aconfig.QuotasParams{} }
func (*quotasManager) ValidateConfig(adapter.AspectConfig) (ce *adapter.ConfigErrors) { return }

func (w *quotasWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*Output, error) {
	// labels holds the generated attributes from mapper
	labels := make(map[string]interface{})
	for attr, e := range w.inputs {
		if val, err := mapper.Eval(e, attrs); err == nil {
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

	// TODO: for now, assume no one passed in the amount
	amount := int64(1)

	qa := adapter.QuotaArgs{
		Name:            d.Name,
		Labels:          make(map[string]interface{}),
		QuotaAmount:     amount,
		DeduplicationID: dedupID,
	}

	/*
		for _, a := range d.Labels {
			if val, ok := labels[a]; ok {
				qa.Labels[a] = val
				continue
			}
			if val, found := attribute.Value(attrs, a); found {
				qa.Labels[a] = val
			}
		}
	*/
	return qa
}

func (w *quotasWrapper) Close() error {
	return w.aspect.Close()
}
