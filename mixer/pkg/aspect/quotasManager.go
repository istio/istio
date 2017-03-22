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

package aspect

import (
	"fmt"

	ptypes "github.com/gogo/protobuf/types"
	"github.com/golang/glog"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
)

type (
	quotasManager struct{}

	quotaInfo struct {
		definition *adapter.QuotaDefinition
		labels     map[string]string
	}

	quotasWrapper struct {
		manager  *quotasManager
		aspect   adapter.QuotasAspect
		metadata map[string]*quotaInfo
		adapter  string
	}
)

// newQuotasManager returns a manager for the quotas aspect.
func newQuotasManager() Manager {
	return &quotasManager{}
}

// NewAspect creates a quota aspect.
func (m *quotasManager) NewAspect(c *cpb.Combined, a adapter.Builder, env adapter.Env) (Wrapper, error) {
	params := c.Aspect.Params.(*aconfig.QuotasParams)

	// TODO: get this from config
	if len(params.Quotas) == 0 {
		params = &aconfig.QuotasParams{
			Quotas: []*aconfig.QuotasParams_Quota{
				{DescriptorName: "RequestCount"},
			},
		}
	}

	// TODO: get this from config
	desc := []dpb.QuotaDescriptor{
		{
			Name:       "RequestCount",
			MaxAmount:  5,
			Expiration: &ptypes.Duration{Seconds: 1},
		},
	}

	metadata := make(map[string]*quotaInfo, len(desc))
	defs := make(map[string]*adapter.QuotaDefinition, len(desc))
	for _, d := range desc {
		quota := findQuota(params.Quotas, d.Name)
		if quota == nil {
			env.Logger().Warningf("No quota found for descriptor %s, skipping it", d.Name)
			continue
		}

		// TODO: once we plumb descriptors into the validation, remove this err: no descriptor should make it through validation
		// if it cannot be converted into a QuotaDefinition, so we should never have to handle the error case.
		def, err := quotaDefinitionFromProto(&d)
		if err != nil {
			_ = env.Logger().Errorf("Failed to convert quota descriptor '%s' to definition with err: %s; skipping it.", d.Name, err)
			continue
		}

		defs[d.Name] = def
		metadata[d.Name] = &quotaInfo{
			labels:     quota.Labels,
			definition: def,
		}
	}

	asp, err := a.(adapter.QuotasBuilder).NewQuotasAspect(env, c.Builder.Params.(adapter.Config), defs)
	if err != nil {
		return nil, err
	}

	return &quotasWrapper{
		manager:  m,
		metadata: metadata,
		aspect:   asp,
		adapter:  a.Name(),
	}, nil
}

func (*quotasManager) Kind() Kind                         { return QuotasKind }
func (*quotasManager) DefaultConfig() config.AspectParams { return &aconfig.QuotasParams{} }
func (*quotasManager) ValidateConfig(config.AspectParams, expr.Validator, descriptor.Finder) (ce *adapter.ConfigErrors) {
	return
}

func (w *quotasWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator, ma APIMethodArgs) Output {
	qma := ma.(*QuotaMethodArgs)

	info, ok := w.metadata[qma.Quota]
	if !ok {
		msg := fmt.Sprintf("Unknown quota '%s' requested", qma.Quota)
		glog.Error(msg)
		return Output{Status: status.WithInvalidArgument(msg)}
	}

	labels, err := evalAll(info.labels, attrs, mapper)
	if err != nil {
		msg := fmt.Sprintf("Unable to evaluate labels for quota '%s' with err: %s", qma.Quota, err)
		glog.Error(msg)
		return Output{Status: status.WithInvalidArgument(msg)}
	}

	qa := adapter.QuotaArgs{
		Definition:      info.definition,
		Labels:          labels,
		QuotaAmount:     qma.Amount,
		DeduplicationID: qma.DeduplicationID,
	}

	var qr adapter.QuotaResult

	if glog.V(2) {
		glog.Info("Invoking adapter %s for quota %s with amount %d", w.adapter, qa.Definition.Name, qa.QuotaAmount)
	}

	if qma.BestEffort {
		qr, err = w.aspect.AllocBestEffort(qa)
	} else {
		qr, err = w.aspect.Alloc(qa)
	}

	if err != nil {
		glog.Errorf("Quota allocation failed: %v", err)
		return Output{Status: status.WithError(err)}
	}

	if qr.Amount == 0 {
		msg := fmt.Sprintf("Unable to allocate %v units from quota %s", qa.QuotaAmount, qa.Definition.Name)
		glog.Warning(msg)
		return Output{Status: status.WithResourceExhausted(msg)}
	}

	if glog.V(2) {
		glog.Infof("Allocate %v units from quota %s", qa.QuotaAmount, qa.Definition.Name)
	}

	return Output{
		Status: status.OK,
		Response: &QuotaMethodResp{
			Amount:     qr.Amount,
			Expiration: qr.Expiration,
		}}
}

func (w *quotasWrapper) Close() error {
	return w.aspect.Close()
}

func findQuota(quotas []*aconfig.QuotasParams_Quota, name string) *aconfig.QuotasParams_Quota {
	for _, q := range quotas {
		if q.DescriptorName == name {
			return q
		}
	}
	return nil
}

func quotaDefinitionFromProto(desc *dpb.QuotaDescriptor) (*adapter.QuotaDefinition, error) {
	labels := make(map[string]adapter.LabelType, len(desc.Labels))
	for _, label := range desc.Labels {
		l, err := valueTypeToLabelType(label.ValueType)
		if err != nil {
			return nil, fmt.Errorf("descriptor '%s' label '%s' failed to convert label type value '%v' from proto with err: %s",
				desc.Name, label.Name, label.ValueType, err)
		}
		labels[label.Name] = l
	}

	dur, _ := ptypes.DurationFromProto(desc.Expiration)
	return &adapter.QuotaDefinition{
		MaxAmount:   desc.MaxAmount,
		Expiration:  dur,
		Description: desc.Description,
		DisplayName: desc.DisplayName,
		Name:        desc.Name,
		Labels:      labels,
	}, nil
}
