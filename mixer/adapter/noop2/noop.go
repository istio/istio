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

package noop2 // import "istio.io/mixer/adapter/noop2"

// NOTE: This adapter will eventually be auto-generated so that it automatically supports all templates
//       known to Mixer. For now, it's manually curated.

import (
	"context"
	"time"

	"github.com/gogo/protobuf/types"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	pkgHndlr "istio.io/mixer/pkg/handler"
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

func (*handler) HandleQuota(ctx context.Context, _ *quota.Instance, args adapter.QuotaRequestArgs) (adapter.QuotaResult2, error) {
	return adapter.QuotaResult2{
			ValidDuration: 1000000000 * time.Second,
			Amount:        args.QuotaAmount,
		},
		nil
}

func (*handler) Close() error { return nil }

////////////////// Config //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() pkgHndlr.Info {
	return pkgHndlr.Info{
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

		// TO BE DELETED
		CreateHandlerBuilder: func() adapter.HandlerBuilder { return &builder{} },
		ValidateConfig: func(cfg adapter.Config) *adapter.ConfigErrors {
			return validateConfig(&pkgHndlr.HandlerConfig{AdapterConfig: cfg})
		},

		ValidateConfig2: validateConfig,
		NewHandler:      newHandler,
	}
}

func validateConfig(*pkgHndlr.HandlerConfig) (ce *adapter.ConfigErrors) {
	return
}

func newHandler(context.Context, adapter.Env, *pkgHndlr.HandlerConfig) (adapter.Handler, error) {
	return &handler{}, nil
}

// EVERYTHING BELOW IS TO BE DELETED

type builder struct{}

// Build is to be deleted
func (b *builder) Build(cfg adapter.Config, env adapter.Env) (adapter.Handler, error) {
	hc := &pkgHndlr.HandlerConfig{
		AdapterConfig: cfg,
	}
	return newHandler(context.Background(), env, hc)
}

// ConfigureCheckNothingHandler is to be deleted
func (*builder) ConfigureCheckNothingHandler(map[string]*checknothing.Type) error {
	return nil
}

// ConfigureReportNothingHandler is to be deleted
func (*builder) ConfigureReportNothingHandler(map[string]*reportnothing.Type) error {
	return nil
}

// ConfigureListEntryHandler is to be deleted
func (*builder) ConfigureListEntryHandler(map[string]*listentry.Type) error {
	return nil
}

// ConfigureLogEntryHandler is to be deleted
func (*builder) ConfigureLogEntryHandler(map[string]*logentry.Type) error {
	return nil
}

// ConfigureMetricHandler is to be deleted
func (*builder) ConfigureMetricHandler(map[string]*metric.Type) error {
	return nil
}

// ConfigureQuotaHandler is to be deleted
func (*builder) ConfigureQuotaHandler(map[string]*quota.Type) error {
	return nil
}
