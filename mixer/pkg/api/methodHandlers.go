// Copyright 2016 Google Inc.
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

package api

// This is what implements the per-request logic for each API method.

import (
	"context"

	"github.com/golang/glog"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/genproto/googleapis/rpc/status"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"

	"fmt"

	mixerpb "istio.io/api/mixer/v1"
)

// MethodHandlers holds pointers to the functions that implement
// request-level processing for incoming all public APIs.
type MethodHandlers interface {
	// Check performs the configured set of precondition checks.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Check(context.Context, attribute.Tracker, *mixerpb.CheckRequest, *mixerpb.CheckResponse)

	// Report performs the requested set of reporting operations.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Report(context.Context, attribute.Tracker, *mixerpb.ReportRequest, *mixerpb.ReportResponse)

	// Quota increments, decrements, or queries the specified quotas.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Quota(context.Context, attribute.Tracker, *mixerpb.QuotaRequest, *mixerpb.QuotaResponse)
}

// Method constants are used to refer to the methods handled by MethodHandlers
type Method int

const (
	// Check represents MethodHandlers.Check
	Check Method = iota
	// Report represents MethodHandlers.Report
	Report
	// Quota represents MethodHandlers.Quota
	Quota
)

// StaticBinding contains all of the pieces required to wire up an adapter to serve traffic in the mixer.
type StaticBinding struct {
	RegisterFn adapter.RegisterFn
	Manager    aspect.Manager
	Config     *aspect.CombinedConfig
	Methods    []Method
}

type methodHandlers struct {
	mngr *adapterManager.Manager
	eval expr.Evaluator

	// Configs for the aspects that'll be used to serve each API method.
	configs map[Method][]*aspect.CombinedConfig
}

// NewMethodHandlers returns a canonical MethodHandlers that implements all of the mixer's API surface
func NewMethodHandlers(bindings ...StaticBinding) MethodHandlers {
	managers := make([]aspect.Manager, len(bindings))
	configs := map[Method][]*aspect.CombinedConfig{Check: {}, Report: {}, Quota: {}}

	for i, binding := range bindings {
		managers[i] = binding.Manager
	}
	adapterMgr := adapterManager.NewManager(managers)
	registry := adapterMgr.Registry()

	for _, binding := range bindings {
		if err := binding.RegisterFn(registry); err != nil {
			panic(fmt.Errorf("failed to register binding '%s' with err: %s", binding.Config.Builder.Name, err))
		}
		for _, method := range binding.Methods {
			configs[method] = append(configs[method], binding.Config)
		}
	}

	return &methodHandlers{
		mngr:    adapterMgr,
		eval:    expr.NewIdentityEvaluator(),
		configs: configs,
	}
}

// does the standard attribute dance for each request
func (h *methodHandlers) execute(ctx context.Context, tracker attribute.Tracker, attrs *mixerpb.Attributes, method Method) *status.Status {
	ab, err := tracker.StartRequest(attrs)
	if err != nil {
		glog.Warningf("Unable to process attribute update. error: '%v'", err)
		return newStatus(code.Code_INVALID_ARGUMENT)
	}
	defer tracker.EndRequest()

	// get a new context with the attribute bag attached
	ctx = attribute.NewContext(ctx, ab)
	for _, conf := range h.configs[method] {
		// TODO: plumb ctx through uber.manager.Execute
		_ = ctx
		out, err := h.mngr.Execute(conf, ab, h.eval)
		if err != nil {
			errorStr := fmt.Sprintf("Adapter %s returned err: %v", conf.Builder.Name, err)
			glog.Warning(errorStr)
			return newStatusWithMessage(code.Code_INTERNAL, errorStr)
		}
		if out.Code != code.Code_OK {
			return newStatusWithMessage(out.Code, "Rejected by builder "+conf.Builder.Name)
		}
	}
	return newStatus(code.Code_OK)
}

func (h *methodHandlers) Check(ctx context.Context, tracker attribute.Tracker, request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = h.execute(ctx, tracker, request.AttributeUpdate, Check)
}

func (h *methodHandlers) Report(ctx context.Context, tracker attribute.Tracker, request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = h.execute(ctx, tracker, request.AttributeUpdate, Report)
}

func (h *methodHandlers) Quota(ctx context.Context, tracker attribute.Tracker, request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	response.RequestIndex = request.RequestIndex
	status := h.execute(ctx, tracker, request.AttributeUpdate, Quota)
	response.Result = newQuotaError(code.Code(status.Code))
}

func newStatus(c code.Code) *status.Status {
	return &status.Status{Code: int32(c)}
}

func newStatusWithMessage(c code.Code, message string) *status.Status {
	return &status.Status{Code: int32(c), Message: message}
}

func newQuotaError(c code.Code) *mixerpb.QuotaResponse_Error {
	return &mixerpb.QuotaResponse_Error{Error: newStatus(c)}
}
