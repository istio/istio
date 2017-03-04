// Copyright 2016 Istio Authors
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
	"fmt"
	"sync/atomic"

	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/status"
)

// Handler holds pointers to the functions that implement
// request-level processing for all public APIs.
type Handler interface {
	// Check performs the configured set of precondition checks.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Check(context.Context, attribute.Tracker, *mixerpb.CheckRequest, *mixerpb.CheckResponse)

	// Report performs the requested set of reporting operations.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Report(context.Context, attribute.Tracker, *mixerpb.ReportRequest, *mixerpb.ReportResponse)

	// Quota increments the specified quotas.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Quota(context.Context, attribute.Tracker, *mixerpb.QuotaRequest, *mixerpb.QuotaResponse)
}

// Executor executes any aspect as described by config.Combined.
type Executor interface {
	// Execute takes a set of configurations and Executes all of them.
	Execute(ctx context.Context, cfgs []*config.Combined, attrs attribute.Bag, ma aspect.APIMethodArgs) aspect.Output
}

// handlerState holds state and configuration for the handler.
type handlerState struct {
	aspectExecutor Executor
	// Configs for the aspects that'll be used to serve each API method. <*config.Runtime>
	cfg atomic.Value

	// methodMap maps an API method to a set of aspects configured for the method
	methodMap map[aspect.APIMethod]config.AspectSet
}

// NewHandler returns a canonical Handler that implements all of the mixer's API surface
func NewHandler(aspectExecutor Executor, methodMap map[aspect.APIMethod]config.AspectSet) Handler {
	return &handlerState{
		aspectExecutor: aspectExecutor,
		methodMap:      methodMap,
	}
}

// execute performs common functions shared across the api surface.
func (h *handlerState) execute(ctx context.Context, tracker attribute.Tracker, attrs *mixerpb.Attributes,
	method aspect.APIMethod, ma aspect.APIMethodArgs) rpc.Status {
	ab, err := tracker.StartRequest(attrs)
	if err != nil {
		msg := fmt.Sprintf("Unable to process attribute update: %v", err)
		glog.Error(msg)
		return status.WithInvalidArgument(msg)
	}
	defer tracker.EndRequest()

	// get a new context with the attribute bag attached
	ctx = attribute.NewContext(ctx, ab)

	untypedCfg := h.cfg.Load()
	if untypedCfg == nil {
		// config has NOT been loaded yet
		const msg = "Configuration is not available"
		glog.Error(msg)
		return status.WithInternal(msg)
	}
	cfg := untypedCfg.(config.Resolver)
	cfgs, err := cfg.Resolve(ab, h.methodMap[method])
	if err != nil {
		msg := fmt.Sprintf("unable to resolve config: %v", err)
		glog.Error(msg)
		return status.WithInternal(msg)
	}

	if glog.V(2) {
		glog.Infof("Resolved [%d] ==> %v ", len(cfgs), cfgs)
	}

	return h.aspectExecutor.Execute(ctx, cfgs, ab, ma).Status
}

// Check performs 'check' function corresponding to the mixer api.
func (h *handlerState) Check(ctx context.Context, tracker attribute.Tracker, request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	if glog.V(2) {
		glog.Infof("Check [%x]", request.RequestIndex)
	}

	s := h.execute(ctx, tracker, request.AttributeUpdate, aspect.CheckMethod, &aspect.CheckMethodArgs{})
	response.RequestIndex = request.RequestIndex
	response.Result = &s

	if glog.V(2) {
		glog.Infof("Check [%x] <-- %s", request.RequestIndex, response)
	}
}

// Report performs 'report' function corresponding to the mixer api.
func (h *handlerState) Report(ctx context.Context, tracker attribute.Tracker, request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	if glog.V(2) {
		glog.Infof("Report [%x]", request.RequestIndex)
	}

	s := h.execute(ctx, tracker, request.AttributeUpdate, aspect.ReportMethod, &aspect.ReportMethodArgs{})
	response.RequestIndex = request.RequestIndex
	response.Result = &s

	if glog.V(2) {
		glog.Infof("Report [%x] <-- %s", request.RequestIndex, response)
	}
}

// Quota performs 'quota' function corresponding to the mixer api.
func (h *handlerState) Quota(ctx context.Context, tracker attribute.Tracker, request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	if glog.V(2) {
		glog.Infof("Quota [%x]", request.RequestIndex)
	}

	response.RequestIndex = request.RequestIndex
	status := h.execute(ctx, tracker, request.AttributeUpdate, aspect.QuotaMethod,
		&aspect.QuotaMethodArgs{
			Quota:           request.Quota,
			Amount:          request.Amount,
			DeduplicationID: request.DeduplicationId,
			BestEffort:      request.BestEffort,
		})

	if status.Code == int32(rpc.OK) {
		response.Amount = 1
	}

	if glog.V(2) {
		glog.Infof("Quota [%x] <-- %s", request.RequestIndex, response)
	}
}

// ConfigChange listens for config change notifications.
func (h *handlerState) ConfigChange(cfg config.Resolver) {
	h.cfg.Store(cfg)
}
