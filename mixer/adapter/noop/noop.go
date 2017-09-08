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

package noop // import "istio.io/mixer/adapter/noop"

// NOTE: This adapter will eventually be auto-generated so that it automatically supports all templates
//       known to Mixer. For now, it's manually curated.

import (
	"context"
	"time"

	"github.com/gogo/protobuf/types"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/template/checknothing"
	"istio.io/mixer/template/listentry"
	"istio.io/mixer/template/logentry"
	"istio.io/mixer/template/metric"
	"istio.io/mixer/template/quota"
	"istio.io/mixer/template/reportnothing"
)

type handler struct{}

var checkResult = adapter.CheckResult{
	Status:        rpc.Status{Code: int32(rpc.OK)},
	ValidDuration: 1000000000 * time.Second,
	ValidUseCount: 1000000000,
}

func (*handler) HandleCheckNothing(context.Context, *checknothing.Instance) (adapter.CheckResult, error) {
	return checkResult, nil
}

func (*handler) HandleReportNothing(context.Context, []*reportnothing.Instance) error {
	return nil
}

func (*handler) HandleListEntry(context.Context, *listentry.Instance) (adapter.CheckResult, error) {
	return checkResult, nil
}

func (*handler) HandleLogEntry(context.Context, []*logentry.Instance) error {
	return nil
}

func (*handler) HandleMetric(context.Context, []*metric.Instance) error {
	return nil
}

func (*handler) HandleQuota(ctx context.Context, _ *quota.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return adapter.QuotaResult{
			ValidDuration: 1000000000 * time.Second,
			Amount:        args.QuotaAmount,
		},
		nil
}

func (*handler) Close() error { return nil }

////////////////// Config //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "noop",
		Impl:        "istio.io/mixer/adapter/noop",
		Description: "Does nothing (useful for testing)",
		SupportedTemplates: []string{
			checknothing.TemplateName,
			reportnothing.TemplateName,
			listentry.TemplateName,
			logentry.TemplateName,
			metric.TemplateName,
			quota.TemplateName,
		},
		DefaultConfig: &types.Empty{},

		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
	}
}

type builder struct{}

func (*builder) SetCheckNothingTypes(map[string]*checknothing.Type)   {}
func (*builder) SetReportNothingTypes(map[string]*reportnothing.Type) {}
func (*builder) SetListEntryTypes(map[string]*listentry.Type)         {}
func (*builder) SetLogEntryTypes(map[string]*logentry.Type)           {}
func (*builder) SetMetricTypes(map[string]*metric.Type)               {}
func (*builder) SetQuotaTypes(map[string]*quota.Type)                 {}
func (*builder) SetAdapterConfig(adapter.Config)                      {}
func (*builder) Validate() (ce *adapter.ConfigErrors)                 { return }

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	return &handler{}, nil
}
