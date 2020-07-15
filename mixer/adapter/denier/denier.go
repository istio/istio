// Copyright Istio Authors.
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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/denier/config/config.proto -x "-n denier -t checknothing -t listentry -t quota -d example"

// Package denier provides an adapter that will return a status code (typically
// FAILED_PRECONDITION) for all calls. It implements the checkNothing, quota and
// listEntry templates.
package denier // import "istio.io/istio/mixer/adapter/denier"

// NOTE: This adapter will eventually be auto-generated so that it automatically supports all CHECK and QUOTA
//       templates known to Mixer. For now, it's manually curated.

import (
	"context"
	"time"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/mixer/adapter/denier/config"
	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/checknothing"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/quota"
)

type handler struct {
	result adapter.CheckResult
}

func defaultParam() *config.Params {
	return &config.Params{
		Status:        status.New(rpc.FAILED_PRECONDITION),
		ValidDuration: 5 * time.Second,
		ValidUseCount: 1000,
	}
}

func newResult(c *config.Params) adapter.CheckResult {
	return adapter.CheckResult{
		Status:        c.Status,
		ValidDuration: c.ValidDuration,
		ValidUseCount: c.ValidUseCount,
	}
}

////////////////// Runtime Methods //////////////////////////

func (h *handler) HandleCheckNothing(context.Context, *checknothing.Instance) (adapter.CheckResult, error) {
	return h.result, nil
}

func (h *handler) HandleListEntry(context.Context, *listentry.Instance) (adapter.CheckResult, error) {
	return h.result, nil
}

func (*handler) HandleQuota(context.Context, *quota.Instance, adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return adapter.QuotaResult{}, nil
}

func (*handler) Close() error { return nil }

////////////////// Bootstrap //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("denier")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}

type builder struct {
	adapterConfig *config.Params
}

func (*builder) SetCheckNothingTypes(map[string]*checknothing.Type) {}
func (*builder) SetListEntryTypes(map[string]*listentry.Type)       {}
func (*builder) SetQuotaTypes(map[string]*quota.Type)               {}
func (b *builder) SetAdapterConfig(cfg adapter.Config)              { b.adapterConfig = cfg.(*config.Params) }
func (*builder) Validate() (ce *adapter.ConfigErrors)               { return }

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	return &handler{
		result: newResult(b.adapterConfig),
	}, nil
}
