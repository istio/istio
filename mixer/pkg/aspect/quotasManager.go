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

func (m *quotasManager) NewQuotaExecutor(c *cpb.Combined, createAspect CreateAspectFunc, env adapter.Env,
	df descriptor.Finder, _ string) (QuotaExecutor, error) {
	params := c.Aspect.Params.(*aconfig.QuotasParams)

	metadata := make(map[string]*quotaInfo, len(params.Quotas))
	defs := make(map[string]*adapter.QuotaDefinition, len(params.Quotas))
	for _, quota := range params.Quotas {
		// We don't check the err because ValidateConfig ensures we have all the descriptors we need and that
		// they can be transformed into their adapter representation.
		def, _ := quotaDefinitionFromProto(df.GetQuota(quota.DescriptorName))
		def.MaxAmount = quota.MaxAmount
		def.Expiration = quota.Expiration

		defs[def.Name] = def
		metadata[def.Name] = &quotaInfo{
			definition: def,
			labels:     quota.Labels,
		}
	}
	out, err := createAspect(env, c.Builder.Params.(adapter.Config), defs)
	if err != nil {
		return nil, err
	}
	asp, ok := out.(adapter.QuotasAspect)
	if !ok {
		return nil, fmt.Errorf("wrong aspect type returned after creation; expected QuotasAspect: %#v", out)
	}

	return &quotasExecutor{
		manager:  m,
		metadata: metadata,
		aspect:   asp,
		adapter:  c.Builder.Name,
	}, nil
}

func (*quotasManager) Kind() config.Kind                  { return config.QuotasKind }
func (*quotasManager) DefaultConfig() config.AspectParams { return &aconfig.QuotasParams{} }

func (*quotasManager) ValidateConfig(c config.AspectParams, tc expr.TypeChecker, df descriptor.Finder) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.QuotasParams)
	for _, quota := range cfg.Quotas {
		desc := df.GetQuota(quota.DescriptorName)
		if desc == nil {
			ce = ce.Appendf("quotas", "could not find a descriptor for the quota '%s'", quota.DescriptorName)
			continue // we can't do any other validation without the descriptor
		}
		ce = ce.Extend(validateLabels(fmt.Sprintf("quotas[%s].labels", desc.Name), quota.Labels, desc.Labels, tc, df))

		if _, err := quotaDefinitionFromProto(desc); err != nil {
			ce = ce.Appendf(fmt.Sprintf("descriptor[%s]", desc.Name), "failed to marshal descriptor into its adapter representation: %v", err)
		}

		if quota.MaxAmount < 0 {
			ce = ce.Appendf("maxAmount", "must be >= 0")
		}

		if quota.Expiration < 0 {
			ce = ce.Appendf("expiration", "cannot be less than 0")
		}

		if desc.RateLimit {
			if quota.Expiration == 0 {
				ce = ce.Appendf("expiration", "must be > 0 for rate limit quotas")
			}
		} else if quota.Expiration != 0 {
			ce = ce.Appendf("expiration", "must be 0 for allocation quotas")
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
		msg := fmt.Sprintf("Unable to evaluate labels for quota '%s': %v", qma.Quota, err)
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
		glog.Infof("Invoking adapter %s for quota %s with amount %d, labels %v", w.adapter, qa.Definition.Name, qa.QuotaAmount, qa.Labels)
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
		glog.Infof("Allocated %v units from quota %s", qa.QuotaAmount, qa.Definition.Name)
	}

	qmr := QuotaMethodResp(qr)
	return status.OK, &qmr
}

func (w *quotasExecutor) Close() error {
	return w.aspect.Close()
}

func quotaDefinitionFromProto(desc *dpb.QuotaDescriptor) (*adapter.QuotaDefinition, error) {
	labels := make(map[string]adapter.LabelType, len(desc.Labels))
	for name, labelType := range desc.Labels {
		l, err := valueTypeToLabelType(labelType)
		if err != nil {
			return nil, fmt.Errorf("descriptor '%s' label '%v' failed to convert label type value '%v' from proto: %v",
				desc.Name, name, labelType, err)
		}
		labels[name] = l
	}

	return &adapter.QuotaDefinition{
		Description: desc.Description,
		DisplayName: desc.DisplayName,
		Name:        desc.Name,
		Labels:      labels,
	}, nil
}
