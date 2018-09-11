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

package servicecontrol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	multierror "github.com/hashicorp/go-multierror"
	sc "google.golang.org/api/servicecontrol/v1"

	"istio.io/istio/mixer/adapter/servicecontrol/config"
	"istio.io/istio/mixer/adapter/servicecontrol/template/servicecontrolreport"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/apikey"
	"istio.io/istio/mixer/template/quota"
	"istio.io/istio/pkg/cache"
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
		ProcessReport(ctx context.Context, instances []*servicecontrolreport.Instance) error
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
		checkDataShape     map[string]*apikey.Type
		reportDataShape    map[string]*servicecontrolreport.Type
		checkResponseCache cache.ExpiringCache // A LRU cache for check response
		client             serviceControlClient
	}

	handler struct {
		ctx *handlerContext

		// lock protects svcProcMap.
		lock sync.Mutex
		// Istio mesh service name to serviceProcessor map. Each serviceProcessor instance handles a single
		// service.
		svcProcMap map[string]*serviceProcessor
	}
)

func newServiceProcessor(meshServiceName string, ctx *handlerContext) (*serviceProcessor, error) {
	checkProc, err := newCheckProcessor(meshServiceName, ctx)
	if err != nil {
		return nil, err
	}

	reportProc, err := newReportProcessor(meshServiceName, ctx, checkProc)
	if err != nil {
		return nil, err
	}
	quotaProc, err := newQuotaProcessor(meshServiceName, ctx)
	if err != nil {
		return nil, err
	}

	return &serviceProcessor{
		checkProcessor:  checkProc,
		reportProcessor: reportProc,
		quotaProcessor:  quotaProc,
	}, nil
}

// HandleApiKey handles apikey check.
// nolint:golint
// Disable lint warning of HandleApiKey name
func (h *handler) HandleApiKey(ctx context.Context, instance *apikey.Instance) (adapter.CheckResult, error) {
	svcProc, err := h.getServiceProcessor(instance.Api)
	if err != nil {
		return adapter.CheckResult{
			Status: status.WithPermissionDenied(err.Error()),
		}, nil
	}
	return svcProc.ProcessCheck(ctx, instance)
}

// HandleServicecontrolReport handles reporting metrics and logs.
func (h *handler) HandleServicecontrolReport(ctx context.Context, instances []*servicecontrolreport.Instance) error {
	// TODO: this is inefficient as it dispatches each report individually, instead of grouping them by the service
	for _, instance := range instances {
		svcProc, err := h.getServiceProcessor(instance.ApiService)
		if err != nil {
			return err
		}
		if err = svcProc.ProcessReport(ctx, []*servicecontrolreport.Instance{instance}); err != nil {
			return err
		}
	}
	return nil
}

// HandleQuota handles rate limiting quota.
func (h *handler) HandleQuota(ctx context.Context, instance *quota.Instance,
	args adapter.QuotaArgs) (adapter.QuotaResult, error) {

	/*
		svcProc, err := h.getServiceProcessor(instance.Api)
		if err != nil {
			return adapter.QuotaResult{
				// This map to rpc.INTERNAL.
				Status: status.WithError(err),
			}, nil
		}
		return svcProc.ProcessQuota(ctx, instance, args)
	*/
	return adapter.QuotaResult{}, errors.New("not implemented")
}

// Close closes a handler.
// TODO(manlinl): Run svcProc.Close in goroutine after reportProcessor implements buffering.
func (h *handler) Close() error {
	var errors *multierror.Error

	h.lock.Lock()
	defer h.lock.Lock()
	for _, svcProc := range h.svcProcMap {
		if err := svcProc.Close(); err != nil {
			errors = multierror.Append(errors, err)
		}
	}

	return errors.ErrorOrNil()
}

func (h *handler) getServiceProcessor(serviceFullName string) (*serviceProcessor, error) {
	if _, ok := h.ctx.serviceConfigIndex[serviceFullName]; !ok {
		return nil, fmt.Errorf("unknown service %v", serviceFullName)
	}

	h.lock.Lock()
	defer h.lock.Unlock()
	svcProc, found := h.svcProcMap[serviceFullName]
	if found {
		return svcProc, nil
	}

	svcProc, err := newServiceProcessor(serviceFullName, h.ctx)
	if err != nil {
		return nil, err
	}
	h.svcProcMap[serviceFullName] = svcProc
	return svcProc, nil
}

func newHandler(ctx *handlerContext) (*handler, error) {
	return &handler{
		ctx:        ctx,
		svcProcMap: make(map[string]*serviceProcessor, len(ctx.config.ServiceConfigs)),
	}, nil
}
