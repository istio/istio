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
	"time"

	"github.com/golang/glog"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
)

// Handler holds pointers to the functions that implement
// request-level processing for all public APIs.
type Handler interface {
	// Check performs the configured set of precondition checks.
	Check(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
		request *mixerpb.CheckRequest, response *mixerpb.CheckResponse)

	// Report performs the requested set of reporting operations.
	Report(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
		request *mixerpb.ReportRequest, response *mixerpb.ReportResponse)

	// Quota increments the specified quotas.
	Quota(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
		request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse)
}

type handlerState struct {
	aspectDispatcher adapterManager.AspectDispatcher
}

// NewHandler returns a canonical Handler that implements all of the mixer's API surface
func NewHandler(aspectDispatcher adapterManager.AspectDispatcher) Handler {
	return &handlerState{
		aspectDispatcher: aspectDispatcher,
	}
}

// Check implements the public Check API.
func (m *handlerState) Check(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	if glog.V(2) {
		glog.Infof("Check [%x]", request.RequestIndex)
	}

	response.RequestIndex = request.RequestIndex
	response.Result = m.aspectDispatcher.Check(ctx, requestBag, responseBag)
	// TODO: this value needs to initially come from config, and be modulated by the kind of attribute
	//       that was used in the check and the in-used aspects (for example, maybe an auth check has a
	//       30s TTL but a whitelist check has got a 120s TTL)
	response.Expiration = time.Duration(5) * time.Second

	if glog.V(2) {
		glog.Infof("Check [%x] <-- %s", request.RequestIndex, response)
	}
}

// Report implements the public Report API.
func (m *handlerState) Report(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	if glog.V(2) {
		glog.Infof("Report [%x]", request.RequestIndex)
	}

	response.RequestIndex = request.RequestIndex
	response.Result = m.aspectDispatcher.Report(ctx, requestBag, responseBag)

	if glog.V(2) {
		glog.Infof("Report [%x] <-- %s", request.RequestIndex, response)
	}
}

// Quota implements the public Quota API.
func (m *handlerState) Quota(ctx context.Context, requestBag *attribute.MutableBag, responseBag *attribute.MutableBag,
	request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	if glog.V(2) {
		glog.Infof("Quota [%x]", request.RequestIndex)
	}

	qma := &aspect.QuotaMethodArgs{
		Quota:           request.Quota,
		Amount:          request.Amount,
		DeduplicationID: request.DeduplicationId,
		BestEffort:      request.BestEffort,
	}
	var qmr *aspect.QuotaMethodResp

	response.RequestIndex = request.RequestIndex
	qmr, response.Result = m.aspectDispatcher.Quota(ctx, requestBag, responseBag, qma)

	if qmr != nil {
		response.Amount = qmr.Amount
		response.Expiration = qmr.Expiration
	}

	if glog.V(2) {
		glog.Infof("Quota [%x] <-- %s", request.RequestIndex, response)
	}
}
