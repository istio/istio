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

package main

// This is what implements the per-request logic for each API method.

import (
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/genproto/googleapis/rpc/status"

	"istio.io/mixer/adapters"

	mixerpb "istio.io/mixer/api/v1"
)

// APIHandlers holds pointers to the functions that implement
// request-level processing for incoming all public APIs.
type APIHandlers interface {
	// Check performs the configured set of precondition checks.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Check(adapters.FactTracker, *mixerpb.CheckRequest, *mixerpb.CheckResponse)

	// Report performs the requested set of reporting operations.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Report(adapters.FactTracker, *mixerpb.ReportRequest, *mixerpb.ReportResponse)

	// Quota increments, decrements, or queries the specified quotas.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Quota(adapters.FactTracker, *mixerpb.QuotaRequest, *mixerpb.QuotaResponse)
}

type apiHandlers struct {
}

func newStatus(c code.Code) *status.Status {
	return &status.Status{Code: int32(c)}
}

func newQuotaError(c code.Code) *mixerpb.QuotaResponse_Error {
	return &mixerpb.QuotaResponse_Error{Error: newStatus(c)}
}

// NewAPIHandlers returns a canonical APIHandlers that implements all of the mixer's API surface
func NewAPIHandlers() APIHandlers {
	return &apiHandlers{}
}

func (h *apiHandlers) Check(tracker adapters.FactTracker, request *mixerpb.CheckRequest, response *mixerpb.CheckResponse) {
	tracker.UpdateFacts(request.GetFacts())
	response.RequestIndex = request.RequestIndex
	response.Result = newStatus(code.Code_UNIMPLEMENTED)
}

func (h *apiHandlers) Report(tracker adapters.FactTracker, request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	tracker.UpdateFacts(request.GetFacts())
	response.RequestIndex = request.RequestIndex
	response.Result = newStatus(code.Code_UNIMPLEMENTED)
}

func (h *apiHandlers) Quota(tracker adapters.FactTracker, request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	tracker.UpdateFacts(request.GetFacts())
	response.RequestIndex = request.RequestIndex
	response.Result = newQuotaError(code.Code_UNIMPLEMENTED)
}
