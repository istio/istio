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

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	config2 "istio.io/mixer/pkg/adapter/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/pkg/template"
)

type (
	quotaManager struct {
		repo template.Repository
	}

	quotaExecutor struct {
		tmplName     string
		procDispatch template.ProcessQuotaFn
		hndlr        config2.Handler
		insts        map[string]proto.Message // instance name -> instance params
	}
)

// NewQuotaManager creates a QuotaManager. TODO make this non public once adapterManager starts using this.
// For now, made it public to please the linter for unused fn error.
func NewQuotaManager(repo template.Repository) QuotaManager {
	return &quotaManager{repo: repo}
}

func (m *quotaManager) NewQuotaExecutor(c *cpb.Combined, createAspect CreateAspectFunc, env adapter.Env,
	df descriptor.Finder, tmpl string) (QuotaExecutor, error) {
	insts := make(map[string]proto.Message)
	for _, cstr := range c.Instances {
		insts[cstr.Name] = cstr.GetParams().(proto.Message)
		if cstr.Template != tmpl {
			return nil, fmt.Errorf("instance's '%v' template is different than expected template name : %s", cstr, tmpl)
		}
	}

	out, err := createAspect(env, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to construct quota aspect with config '%v': %v", c, err)
	}

	// adapter.Aspect is identical to adapter.config.Handler, this cast has to pass.
	v, _ := out.(config2.Handler)

	ti, _ := m.repo.GetTemplateInfo(tmpl)
	if b := ti.HandlerSupportsTemplate(v); !b {
		return nil, fmt.Errorf("candler does not implement interface %s. "+
			"Therefore, it cannot support template %v", ti.HndlrName, tmpl)
	}

	return &quotaExecutor{tmpl, ti.ProcessQuota, v, insts}, nil
}

func (*quotaManager) DefaultConfig() config.AspectParams { return nil }
func (*quotaManager) ValidateConfig(c config.AspectParams, tc expr.TypeChecker, df descriptor.Finder) (ce *adapter.ConfigErrors) {
	return
}

func (*quotaManager) Kind() config.Kind {
	return config.Undefined
}

func (w *quotaExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator, qma *QuotaMethodArgs) (rpc.Status, *QuotaMethodResp) {
	ctr, ok := w.insts[qma.Quota]
	if !ok {
		msg := fmt.Sprintf("unknown quota '%s' requested", qma.Quota)
		glog.Error(msg)
		return status.WithInvalidArgument(msg), nil
	}
	qra := adapter.QuotaRequestArgs{
		QuotaAmount:     qma.Amount,
		DeduplicationID: qma.DeduplicationID,
		BestEffort:      qma.BestEffort,
	}

	s, _, qr := w.procDispatch(qma.Quota, ctr, attrs, mapper, w.hndlr, qra) // ignore Cacheability info for now.
	qmr := QuotaMethodResp(qr)
	return s, &qmr
}

func (w *quotaExecutor) Close() error {
	// Noop: executor does not own the handler, so it cannot close it.
	return nil
}
