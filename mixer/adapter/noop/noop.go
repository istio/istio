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

package noop // import "istio.io/istio/mixer/adapter/noop"

// NOTE: This adapter will eventually be auto-generated so that it automatically supports all templates
//       known to Mixer. For now, it's manually curated.

import (
	"context"
	"time"

	"github.com/gogo/protobuf/types"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/checknothing"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	"istio.io/istio/mixer/template/reportnothing"
	"istio.io/istio/pkg/log"
)

type handler struct{}

var checkResult = adapter.CheckResult{
	Status:        rpc.Status{Code: int32(rpc.OK)},
	ValidDuration: 1000000000 * time.Second,
	ValidUseCount: 1000000000,
}

func (*handler) HandleCheckNothing(ctx context.Context, instance *checknothing.Instance) (adapter.CheckResult, error) {
	if log.DebugEnabled() {
		log.Debugf("NOOP Check: instance: %s", instance.Name)
	}
	return checkResult, nil
}

func (*handler) HandleReportNothing(ctx context.Context, instances []*reportnothing.Instance) error {
	if log.DebugEnabled() {
		// Assuming instances will never be empty!
		log.Debugf("NOOP Report: instances: count:%d i[0]:%s", len(instances), instances[0].Name)
	}
	return nil
}

func (*handler) HandleListEntry(ctx context.Context, instance *listentry.Instance) (adapter.CheckResult, error) {
	if log.DebugEnabled() {
		log.Debugf("NOOP ListEntry: instance: %s => %v", instance.Name, instance)
	}
	return checkResult, nil
}

func (*handler) HandleLogEntry(ctx context.Context, instances []*logentry.Instance) error {
	if log.DebugEnabled() {
		log.Debugf("NOOP LogEntry: instances: count:%d i[0]:%s => %v", len(instances), instances[0].Name, instances[0])
	}
	return nil
}

func (*handler) HandleMetric(ctx context.Context, instances []*metric.Instance) error {
	if log.DebugEnabled() {
		// Assuming instances will never be empty!
		log.Debugf("NOOP HandleMetric: instances: count:%d i[0]:%s => %v", len(instances), instances[0].Name, instances[0])
	}
	return nil
}

func (*handler) HandleQuota(ctx context.Context, instance *quota.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	if log.DebugEnabled() {
		log.Debugf("NOOP Quota: instance: %s => %v, args:'%v'", instance.Name, instance, args)
	}

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
		Impl:        "istio.io/istio/mixer/adapter/noop",
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
