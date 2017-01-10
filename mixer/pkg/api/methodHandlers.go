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

	"istio.io/mixer/pkg/attribute"

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

type methodHandlers struct {
}

func newStatus(c code.Code) *status.Status {
	return &status.Status{Code: int32(c)}
}

func newQuotaError(c code.Code) *mixerpb.QuotaResponse_Error {
	return &mixerpb.QuotaResponse_Error{Error: newStatus(c)}
}

// NewMethodHandlers returns a canonical MethodHandlers that implements all of the mixer's API surface
func NewMethodHandlers() MethodHandlers {
	return &methodHandlers{}
}

type workFunc func(context.Context, attribute.MutableBag) *status.Status

// does the standard attribute dance for each request
func wrapper(ctx context.Context, tracker attribute.Tracker, attrs *mixerpb.Attributes, workFn workFunc) *status.Status {
	ab, err := tracker.StartRequest(attrs)
	if err != nil {
		glog.Warningf("Unable to process attribute update. error: '%v'", err)
		return newStatus(code.Code_INVALID_ARGUMENT)
	}
	defer tracker.EndRequest()

	// get a new context with the attribute bag attached
	ctx = attribute.NewContext(ctx, ab)
	_ = ctx // will eventually be passed down to adapters

	return workFn(ctx, ab)
}

func (h *methodHandlers) Check(ctx context.Context, tracker attribute.Tracker, request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = wrapper(ctx, tracker, request.AttributeUpdate, h.checkWorker)
}

func (h *methodHandlers) checkWorker(ctx context.Context, ab attribute.MutableBag) *status.Status {
	return newStatus(code.Code_UNIMPLEMENTED)
}

func (h *methodHandlers) Report(ctx context.Context, tracker attribute.Tracker, request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = wrapper(ctx, tracker, request.AttributeUpdate, h.reportWorker)
}

func (h *methodHandlers) reportWorker(ctx context.Context, ab attribute.MutableBag) *status.Status {
	return newStatus(code.Code_UNIMPLEMENTED)
}

func (h *methodHandlers) Quota(ctx context.Context, tracker attribute.Tracker, request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	response.RequestIndex = request.RequestIndex
	status := wrapper(ctx, tracker, request.AttributeUpdate, h.quotaWorker)
	response.Result = newQuotaError(code.Code(status.Code))
}

func (h *methodHandlers) quotaWorker(ctx context.Context, ab attribute.MutableBag) *status.Status {
	return newStatus(code.Code_UNIMPLEMENTED)
}
