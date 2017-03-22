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
	"time"

	"github.com/golang/glog"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/status"
)

// Handler holds pointers to the functions that implement
// request-level processing for all public APIs.
type Handler interface {
	// Check performs the configured set of precondition checks.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Check(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
		request *mixerpb.CheckRequest, response *mixerpb.CheckResponse)

	// Report performs the requested set of reporting operations.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Report(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
		request *mixerpb.ReportRequest, response *mixerpb.ReportResponse)

	// Quota increments the specified quotas.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Quota(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
		request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse)
}

// Executor executes any aspect as described by config.Combined.
type Executor interface {
	// Execute takes a set of configurations and Executes all of them.
	Execute(ctx context.Context, cfgs []*cpb.Combined, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
		ma aspect.APIMethodArgs) aspect.Output
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
func (h *handlerState) execute(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	method aspect.APIMethod, ma aspect.APIMethodArgs) aspect.Output {
	// get a new context with the attribute bag attached
	ctx = attribute.NewContext(ctx, requestBag)

	cfg, _ := h.cfg.Load().(config.Resolver)
	if cfg == nil {
		// config has not been loaded yet
		const msg = "Configuration is not yet available"
		glog.Error(msg)
		return aspect.Output{Status: status.WithInternal(msg)}
	}

	cfgs, err := cfg.Resolve(requestBag, h.methodMap[method])
	if err != nil {
		msg := fmt.Sprintf("unable to resolve config: %v", err)
		glog.Error(msg)
		return aspect.Output{Status: status.WithInternal(msg)}
	}

	if glog.V(2) {
		glog.Infof("Resolved [%d] ==> %v ", len(cfgs), cfgs)
	}

	return h.aspectExecutor.Execute(ctx, cfgs, requestBag, responseBag, ma)
}

// Check performs 'check' function corresponding to the mixer api.
func (h *handlerState) Check(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	if glog.V(2) {
		glog.Infof("Check [%x]", request.RequestIndex)
	}

	o := h.execute(ctx, requestBag, responseBag, aspect.CheckMethod, &aspect.CheckMethodArgs{})
	response.RequestIndex = request.RequestIndex
	response.Result = o.Status

	// TODO: this value needs to initially come from config, and be modulated by the kind of attribute
	//       that was used in the check and the in-used aspects (for example, maybe an auth check has a
	//       30s TTL but a whitelist check has got a 120s TTL)
	response.Expiration = time.Duration(5) * time.Second

	if glog.V(2) {
		glog.Infof("Check [%x] <-- %s", request.RequestIndex, response)
	}
}

// Report performs 'report' function corresponding to the mixer api.
func (h *handlerState) Report(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	if glog.V(2) {
		glog.Infof("Report [%x]", request.RequestIndex)
	}

	o := h.execute(ctx, requestBag, responseBag, aspect.ReportMethod, &aspect.ReportMethodArgs{})
	response.RequestIndex = request.RequestIndex
	response.Result = o.Status

	if glog.V(2) {
		glog.Infof("Report [%x] <-- %s", request.RequestIndex, response)
	}
}

// Quota performs 'quota' function corresponding to the mixer api.
func (h *handlerState) Quota(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	if glog.V(2) {
		glog.Infof("Quota [%x]", request.RequestIndex)
	}

	response.RequestIndex = request.RequestIndex
	o := h.execute(ctx, requestBag, responseBag, aspect.QuotaMethod,
		&aspect.QuotaMethodArgs{
			Quota:           request.Quota,
			Amount:          request.Amount,
			DeduplicationID: request.DeduplicationId,
			BestEffort:      request.BestEffort,
		})

	response.Result = o.Status
	if o.IsOK() {
		resp := o.Response.(*aspect.QuotaMethodResp)
		response.Amount = resp.Amount
		response.Expiration = resp.Expiration
	}

	if glog.V(2) {
		glog.Infof("Quota [%x] <-- %s", request.RequestIndex, response)
	}
}

// ConfigChange listens for config change notifications.
func (h *handlerState) ConfigChange(cfg config.Resolver) {
	h.cfg.Store(cfg)
}
