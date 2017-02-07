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

	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"

	"fmt"

	"sync/atomic"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/config"
)

// Handler holds pointers to the functions that implement
// request-level processing for incoming all public APIs.
type Handler interface {
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

// Executor executes any aspect as described by config.Combined.
type Executor interface {
	// Execute takes a set of configurations and Executes all of them.
	Execute(ctx context.Context, cfgs []*config.Combined, attrs attribute.Bag) ([]*aspect.Output, error)
}

// handlerState holds state and configuration for the handler.
type handlerState struct {
	aspectExecutor Executor
	// Configs for the aspects that'll be used to serve each API method. <*config.Runtime>
	cfg atomic.Value

	// methodmap maps apimethod to aspectSet
	methodmap map[config.APIMethod]config.AspectSet
}

// NewHandler returns a canonical Handler that implements all of the mixer's API surface
func NewHandler(aspectExecutor Executor, methodmap map[config.APIMethod]config.AspectSet) Handler {
	return &handlerState{
		aspectExecutor: aspectExecutor,
		methodmap:      methodmap,
	}
}

// execute performs common function shared across the api surface.
func (h *handlerState) execute(ctx context.Context, tracker attribute.Tracker, attrs *mixerpb.Attributes, method config.APIMethod) *status.Status {
	ab, err := tracker.StartRequest(attrs)
	if err != nil {
		glog.Warningf("Unable to process attribute update. error: '%v'", err)
		return newStatus(code.Code_INVALID_ARGUMENT)
	}
	defer tracker.EndRequest()

	// get a new context with the attribute bag attached
	ctx = attribute.NewContext(ctx, ab)

	untypedCfg := h.cfg.Load()
	if untypedCfg == nil {
		const gerr = "configuration is not available"
		// config has NOT been loaded yet
		glog.Error(gerr)
		return newStatusWithMessage(code.Code_INTERNAL, gerr)
	}
	cfg := untypedCfg.(config.Resolver)
	cfgs, err := cfg.Resolve(ab, h.methodmap[method])
	if err != nil {
		return newStatusWithMessage(code.Code_INTERNAL, fmt.Sprintf("unable to resolve config %s", err.Error()))
	}
	if glog.V(2) {
		glog.Infof("Resolved [%d] ==> %v ", len(cfgs), cfgs)
	}

	outs, err := h.aspectExecutor.Execute(ctx, cfgs, ab)
	if err != nil {
		return newStatusWithMessage(code.Code_INTERNAL, err.Error())
	}
	for _, out := range outs {
		if out.Code != code.Code_OK {
			return newStatus(out.Code)
		}
	}
	return newStatus(code.Code_OK)
}

// Check performs 'check' function corresponding to the mixer api.
func (h *handlerState) Check(ctx context.Context, tracker attribute.Tracker, request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = h.execute(ctx, tracker, request.AttributeUpdate, config.CheckMethod)
	if glog.V(2) {
		glog.Infof("Check (%v %v) ==> %v ", tracker, request.AttributeUpdate, response)
	}
}

// Report performs 'report' function corresponding to the mixer api.
func (h *handlerState) Report(ctx context.Context, tracker attribute.Tracker, request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = h.execute(ctx, tracker, request.AttributeUpdate, config.ReportMethod)
	if glog.V(2) {
		glog.Infof("Report (%v %v) ==> %v ", tracker, request.AttributeUpdate, response)
	}
}

// Quota performs 'quota' function corresponding to the mixer api.
func (h *handlerState) Quota(ctx context.Context, tracker attribute.Tracker, request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	response.RequestIndex = request.RequestIndex
	status := h.execute(ctx, tracker, request.AttributeUpdate, config.QuotaMethod)

	if status.Code == int32(code.Code_OK) {
		response.Amount = 1
	}
}

func newStatus(c code.Code) *status.Status {
	return &status.Status{Code: int32(c)}
}

func newStatusWithMessage(c code.Code, message string) *status.Status {
	return &status.Status{Code: int32(c), Message: message}
}

// ConfigChange listens for config change notifications.
func (h *handlerState) ConfigChange(cfg config.Resolver) {
	h.cfg.Store(cfg)
}
