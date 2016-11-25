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

	"github.com/istio/mixer/adapters"

	mixpb "github.com/istio/mixer/api/v1/go"
)

// APIHandlers holds pointers to the functions that implement
// request-level processing for incoming all public APIs.
type APIHandlers interface {
	// Check performs the configured set of precondition checks.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Check(adapters.FactConverter, *mixpb.CheckRequest, *mixpb.CheckResponse)

	// Report performs the requested set of reporting operations.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Report(adapters.FactConverter, *mixpb.ReportRequest, *mixpb.ReportResponse)

	// Quota increments, decrements, or queries the specified quotas.
	// Note that the request parameter is immutable, while the response parameter is where
	// results are specified
	Quota(adapters.FactConverter, *mixpb.QuotaRequest, *mixpb.QuotaResponse)
}

type apiHandlers struct {
}

func newStatus(c code.Code) *status.Status {
	return &status.Status{Code: int32(c)}
}

func newQuotaError(c code.Code) *mixpb.QuotaResponse_Error {
	return &mixpb.QuotaResponse_Error{Error: newStatus(c)}
}

// NewAPIHandlers returns a canonical APIHandlers that implements all of the mixer's API surface
func NewAPIHandlers() APIHandlers {
	return &apiHandlers{}
}

func (h *apiHandlers) Check(conv adapters.FactConverter, request *mixpb.CheckRequest, response *mixpb.CheckResponse) {
	conv.UpdateFacts(request.GetFacts())
	response.RequestId = request.RequestId
	response.Result = newStatus(code.Code_UNIMPLEMENTED)
}

func (h *apiHandlers) Report(conv adapters.FactConverter, request *mixpb.ReportRequest, response *mixpb.ReportResponse) {
	conv.UpdateFacts(request.GetFacts())
	response.RequestId = request.RequestId
	response.Result = newStatus(code.Code_UNIMPLEMENTED)
}

func (h *apiHandlers) Quota(conv adapters.FactConverter, request *mixpb.QuotaRequest, response *mixpb.QuotaResponse) {
	conv.UpdateFacts(request.GetFacts())
	response.RequestId = request.RequestId
	response.Result = newQuotaError(code.Code_UNIMPLEMENTED)
}
