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
	rpc "github.com/googleapis/googleapis/google/rpc"

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

	quotasExecutor struct {
		manager  *quotasManager
		aspect   adapter.QuotasAspect
		metadata map[string]*quotaInfo
		adapter  string
	}
)

// newQuotasManager returns a manager for the quotas aspect.
func newQuotasManager() QuotaManager {
	return &quotasManager{}
}

func (m *quotasManager) NewQuotaExecutor(c *cpb.Combined, a adapter.Builder, env adapter.Env, df descriptor.Finder) (QuotaExecutor, error) {
	params := c.Aspect.Params.(*aconfig.QuotasParams)

	metadata := make(map[string]*quotaInfo, len(params.Quotas))
	defs := make(map[string]*adapter.QuotaDefinition, len(params.Quotas))
	for _, quota := range params.Quotas {
		// We don't check the err because ValidateConfig ensures we have all the descriptors we need and that
		// they can be transformed into their adapter representation.
		def, _ := quotaDefinitionFromProto(df.GetQuota(quota.DescriptorName))
		defs[def.Name] = def
		metadata[def.Name] = &quotaInfo{
			definition: def,
			labels:     quota.Labels,
		}
	}

	asp, err := a.(adapter.QuotasBuilder).NewQuotasAspect(env, c.Builder.Params.(adapter.Config), defs)
	if err != nil {
		return nil, err
	}

	return &quotasExecutor{
		manager:  m,
		metadata: metadata,
		aspect:   asp,
		adapter:  a.Name(),
	}, nil
}

func (*quotasManager) Kind() config.Kind                  { return config.QuotasKind }
func (*quotasManager) DefaultConfig() config.AspectParams { return &aconfig.QuotasParams{} }

func (*quotasManager) ValidateConfig(c config.AspectParams, v expr.Validator, df descriptor.Finder) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.QuotasParams)
	for _, quota := range cfg.Quotas {
		desc := df.GetQuota(quota.DescriptorName)
		if desc == nil {
			ce = ce.Appendf("Quotas", "could not find a descriptor for the quota '%s'", quota.DescriptorName)
			continue // we can't do any other validation without the descriptor
		}
		ce = ce.Extend(validateLabels(fmt.Sprintf("Quotas[%s].Labels", desc.Name), quota.Labels, desc.Labels, v, df))

		if _, err := quotaDefinitionFromProto(desc); err != nil {
			ce = ce.Appendf(fmt.Sprintf("Descriptor[%s]", desc.Name), "failed to marshal descriptor into its adapter representation with err: %v", err)
		}
	}
	return
}

func (w *quotasExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator, qma *QuotaMethodArgs) (rpc.Status, *QuotaMethodResp) {
	info, ok := w.metadata[qma.Quota]
	if !ok {
		msg := fmt.Sprintf("Unknown quota '%s' requested", qma.Quota)
		glog.Error(msg)
		return status.WithInvalidArgument(msg), nil
	}

	labels, err := evalAll(info.labels, attrs, mapper)
	if err != nil {
		msg := fmt.Sprintf("Unable to evaluate labels for quota '%s' with err: %s", qma.Quota, err)
		glog.Error(msg)
		return status.WithInvalidArgument(msg), nil
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
		return status.WithError(err), nil
	}

	if qr.Amount == 0 {
		msg := fmt.Sprintf("Unable to allocate %v units from quota %s", qa.QuotaAmount, qa.Definition.Name)
		glog.Warning(msg)
		return status.WithResourceExhausted(msg), nil
	}

	if glog.V(2) {
		glog.Infof("Allocate %v units from quota %s", qa.QuotaAmount, qa.Definition.Name)
	}

	qmr := QuotaMethodResp(qr)
	return status.OK, &qmr
}

func (w *quotasExecutor) Close() error {
	return w.aspect.Close()
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

	dur, err := ptypes.DurationFromProto(desc.Expiration)
	if err != nil {
		return nil, fmt.Errorf("descriptor '%s' failed to parse duration from proto with err: %v", desc.Name, err)
	}
	return &adapter.QuotaDefinition{
		MaxAmount:   desc.MaxAmount,
		Expiration:  dur,
		Description: desc.Description,
		DisplayName: desc.DisplayName,
		Name:        desc.Name,
		Labels:      labels,
	}, nil
}
