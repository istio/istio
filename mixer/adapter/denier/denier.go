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

package denier // import "istio.io/mixer/adapter/denier"

// NOTE: This adapter will eventually be auto-generated so that it automatically supports all CHECK and QUOTA
//       templates known to Mixer. For now, it's manually curated.

import (
	"context"
	"time"

	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/adapter/denier/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/template/checknothing"
	"istio.io/mixer/template/listentry"
	"istio.io/mixer/template/quota"
)

type (
	builder struct{}

	handler struct {
		status rpc.Status
	}
)

// ensure our types implement the requisite interfaces
var _ checknothing.HandlerBuilder = &builder{}
var _ checknothing.Handler = &handler{}
var _ listentry.HandlerBuilder = &builder{}
var _ listentry.Handler = &handler{}
var _ quota.HandlerBuilder = &builder{}
var _ quota.Handler = &handler{}

///////////////// Configuration Methods ///////////////

func (*builder) Build(cfg adapter.Config, env adapter.Env) (adapter.Handler, error) {
	return &handler{status: cfg.(*config.Params).Status}, nil
}

func (*builder) ConfigureCheckNothingHandler(map[string]*checknothing.Type) error {
	return nil
}

func (*builder) ConfigureListEntryHandler(map[string]*listentry.Type) error {
	return nil
}

func (*builder) ConfigureQuotaHandler(map[string]*quota.Type) error {
	return nil
}

////////////////// Runtime Methods //////////////////////////

func (h *handler) HandleCheckNothing(context.Context, *checknothing.Instance) (adapter.CheckResult, error) {
	return adapter.CheckResult{
		Status:        h.status,
		ValidDuration: 1000 * time.Second,
		ValidUseCount: 1000,
	}, nil
}

func (h *handler) HandleListEntry(context.Context, *listentry.Instance) (adapter.CheckResult, error) {
	return adapter.CheckResult{
		Status:        h.status,
		ValidDuration: 1000 * time.Second,
		ValidUseCount: 1000,
	}, nil
}

func (*handler) HandleQuota(context.Context, *quota.Instance, adapter.QuotaRequestArgs) (adapter.QuotaResult2, error) {
	return adapter.QuotaResult2{}, nil
}

func (*handler) Close() error { return nil }

////////////////// Bootstrap //////////////////////////

// GetBuilderInfo returns the BuilderInfo associated with this adapter implementation.
func GetBuilderInfo() adapter.BuilderInfo {
	return adapter.BuilderInfo{
		Name:        "denier",
		Impl:        "istio.io/mixer/adapter/denier",
		Description: "Rejects any check and quota request with a configurable error",
		SupportedTemplates: []string{
			checknothing.TemplateName,
			listentry.TemplateName,
			quota.TemplateName,
		},
		DefaultConfig: &config.Params{
			Status: rpc.Status{Code: int32(rpc.FAILED_PRECONDITION)},
		},
		CreateHandlerBuilder: func() adapter.HandlerBuilder { return &builder{} },
		ValidateConfig:       func(adapter.Config) *adapter.ConfigErrors { return nil },
	}
}
