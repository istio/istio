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

package svcctrl

import (
	"context"
	"io"

	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/svcctrl/config"
	"istio.io/istio/mixer/adapter/svcctrl/template/svcctrlreport"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/apikey"
	"istio.io/istio/mixer/template/quota"
)

type (
	serviceControlClient interface {
		Check(googleServiceName string, request *sc.CheckRequest) (*sc.CheckResponse, error)
		Report(googleServiceName string, request *sc.ReportRequest) (*sc.ReportResponse, error)
		AllocateQuota(googleServiceName string, request *sc.AllocateQuotaRequest) (*sc.AllocateQuotaResponse, error)
	}

	checkProcessor interface {
		ProcessCheck(ctx context.Context, instance *apikey.Instance) (adapter.CheckResult, error)
	}

	reportProcessor interface {
		io.Closer
		ProcessReport(ctx context.Context, instances []*svcctrlreport.Instance) error
	}

	quotaProcessor interface {
		ProcessQuota(ctx context.Context, instances *quota.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error)
	}

	serviceProcessor struct {
		checkProcessor
		reportProcessor
		quotaProcessor
	}

	handlerContext struct {
		env    adapter.Env
		config *config.Params
		// A map keyed by mesh service name to service config in adapter config
		serviceConfigIndex map[string]*config.GcpServiceSetting

		checkDataShape  map[string]*apikey.Type
		reportDataShape map[string]*svcctrlreport.Type

		client serviceControlClient
	}

	handler struct {
		ctx *handlerContext
		// TODO(manlinl): Switch to a LRU cache of serviceProcessor once Mixer includes destination.service in all
		// instances by default. Then we can enable a single handler to server multiple services.
		svcProc *serviceProcessor
	}
)

func newServiceProcessor(meshServiceName string, ctx *handlerContext) (*serviceProcessor, error) {
	checkProc, err := newCheckProcessor(meshServiceName, ctx)
	if err != nil {
		return nil, err
	}

	quotaProc, err := newQuotaProcessor(meshServiceName, ctx)
	if err != nil {
		return nil, err
	}

	return &serviceProcessor{
		checkProcessor: checkProc,
		quotaProcessor: quotaProc,
	}, nil
}

// HandleApiKey handles apikey check.
func (h *handler) HandleApiKey(ctx context.Context, instance *apikey.Instance) (adapter.CheckResult, error) {
	result, err := h.svcProc.ProcessCheck(ctx, instance)
	logger := h.ctx.env.Logger()
	if err != nil {
		logger.Errorf("svcctrl check failed: %v", err)
	}
	return result, err
}

// HandleSvcctrlReport handles reporting metrics and logs.
func (h *handler) HandleSvcctrlReport(ctx context.Context, instances []*svcctrlreport.Instance) error {
	return nil
}

// HandleQuota handles rate limiting quota.
func (h *handler) HandleQuota(ctx context.Context, instance *quota.Instance,
	args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return h.svcProc.ProcessQuota(ctx, instance, args)
}

// Close closes a serviceProcessor
func (h *handler) Close() error {
	return h.svcProc.Close()
}

func newHandler(ctx *handlerContext) (*handler, error) {
	svcProc, err := newServiceProcessor(ctx.config.ServiceConfigs[0].MeshServiceName, ctx)
	if err != nil {
		return nil, err
	}
	return &handler{ctx, svcProc}, nil
}
