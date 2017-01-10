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
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/server"

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
	adapterManager *server.AdapterManager
	configManager  *server.ConfigManager
}

var okStatus = newStatus(code.Code_OK)

func newStatus(c code.Code) *status.Status {
	return &status.Status{Code: int32(c)}
}

func newQuotaError(c code.Code) *mixerpb.QuotaResponse_Error {
	return &mixerpb.QuotaResponse_Error{Error: newStatus(c)}
}

// NewMethodHandlers returns a canonical MethodHandlers that implements all of the mixer's API surface
func NewMethodHandlers(adapterManager *server.AdapterManager, configManager *server.ConfigManager) MethodHandlers {
	return &methodHandlers{
		adapterManager: adapterManager,
		configManager:  configManager,
	}
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
	dispatchKey, err := server.NewDispatchKey(ab)
	if err != nil {
		glog.Warningf("Error extracting the dispatch key. error: '%v'", err)
		return newStatus(code.Code_FAILED_PRECONDITION)
	}

	allowed, err := h.checkLists(dispatchKey, ab)
	if err != nil {
		glog.Warningf("Unexpected check error. dispatchKey: '%v', error: '%v'", dispatchKey, err)
		return newStatus(code.Code_INTERNAL)
	}

	if !allowed {
		return newStatus(code.Code_PERMISSION_DENIED)
	}

	// No objections from any of the adapters
	return okStatus
}

func (h *methodHandlers) checkLists(dispatchKey server.DispatchKey, ab attribute.MutableBag) (bool, error) {
	// TODO: What is the correct error handling policy for the check calls? This implementation opts for fail-close.
	configBlocks, err := h.configManager.GetListCheckerConfigBlocks(dispatchKey)
	if err != nil {
		return false, err
	}

	for _, configBlock := range configBlocks {
		listCheckerAdapter, err := h.adapterManager.GetListCheckerAdapter(dispatchKey, configBlock.AspectConfig)
		if err != nil {
			return false, err
		}

		symbol, err := "SomeSymbol", nil //configBlock.Evaluator.EvaluateSymbolBinding(tracker.GetLabels())
		if err != nil {
			return false, err
		}

		inList, err := listCheckerAdapter.CheckList(symbol)
		if err != nil {
			return false, err
		}

		if !inList {
			// TODO: listCheckerAdapter is very heavy-weight to log here. We should extract a canonical
			// identifier for the adapter and log it instead.
			glog.Infof("Check call is denied by adapter. dispatchKey: '%v', adapter: '%v'",
				dispatchKey, listCheckerAdapter)
			return false, nil
		}
	}

	return true, nil
}

func (h *methodHandlers) Report(ctx context.Context, tracker attribute.Tracker, request *mixerpb.ReportRequest, response *mixerpb.ReportResponse) {
	response.RequestIndex = request.RequestIndex
	response.Result = wrapper(ctx, tracker, request.AttributeUpdate, h.reportWorker)
}

func (h *methodHandlers) reportWorker(ctx context.Context, ab attribute.MutableBag) *status.Status {
	dispatchKey, err := server.NewDispatchKey(ab)
	if err != nil {
		glog.Warningf("Error extracting the dispatch key. error: '%v'", err)
		return newStatus(code.Code_FAILED_PRECONDITION)
	}

	err = h.report(dispatchKey, ab)
	if err != nil {
		glog.Warningf("Unexpected report error: %v", err)
		return newStatus(code.Code_INTERNAL)
	}

	return okStatus
}

func (h *methodHandlers) report(dispatchKey server.DispatchKey, ac attribute.Bag) error {
	aspectConfigs, err := h.configManager.GetLoggerAspectConfigs(dispatchKey)
	if err != nil {
		return err
	}

	var result error
	for _, aspectConfig := range aspectConfigs {
		loggerAdapter, err := h.adapterManager.GetLoggerAdapter(dispatchKey, aspectConfig)
		if err != nil {
			return err
		}

		convertedLogs := []adapter.LogEntry{}
		err = loggerAdapter.Log(convertedLogs)
		if err != nil {
			if result != nil {
				// TODO: It maybe worthwhile to come up with a way to accomulate errors.
				// TODO: LoggerAdapter is very heavy-weight to log here. We should extract a canonical
				glog.Infof("Unexpected error from logging adapter: error='%v', adapter='%v'", err, loggerAdapter)
				result = err
			}
		}
	}

	return result
}

/*
func buildLogEntries(entries []*mixerpb.LogEntry) []adapters.LogEntry {
	// TODO: actual conversion implementation
	result := make([]adapters.LogEntry, len(entries))
	for i, e := range entries {
		timestamp, err := ptypes.Timestamp(e.GetTimestamp())
		if err != nil {
			glog.Warningf("Error converting the log timestamp: error='%v', timestamp='%v'", err, e.GetTimestamp())
			// Use a "default" timestamp to avoid losing the log information.
			timestamp, _ = ptypes.Timestamp(nil)
		}
		entry := adapters.LogEntry{
			Collection:  e.GetLogCollection(),
			Timestamp:   timestamp,
			TextPayload: e.GetTextPayload(),
			Severity:    e.GetSeverity().String(),
			// TODO: labels
			// TODO: StructPayload conversion
			// TODO: ProtoPayload conversion
		}

		result[i] = entry
	}

	return result
}
*/

func (h *methodHandlers) Quota(ctx context.Context, tracker attribute.Tracker, request *mixerpb.QuotaRequest, response *mixerpb.QuotaResponse) {
	response.RequestIndex = request.RequestIndex
	status := wrapper(ctx, tracker, request.AttributeUpdate, h.quotaWorker)
	response.Result = newQuotaError(code.Code(status.Code))
}

func (h *methodHandlers) quotaWorker(ctx context.Context, ab attribute.MutableBag) *status.Status {
	return newStatus(code.Code_UNIMPLEMENTED)
}
