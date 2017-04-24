/* Copyright 2016 Google Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
#ifndef API_MANAGER_CONTEXT_REQUEST_CONTEXT_H_
#define API_MANAGER_CONTEXT_REQUEST_CONTEXT_H_

#include <time.h>
#include <chrono>
#include <memory>

#include "contrib/endpoints/include/api_manager/method.h"
#include "contrib/endpoints/include/api_manager/request.h"
#include "contrib/endpoints/include/api_manager/response.h"
#include "contrib/endpoints/src/api_manager/cloud_trace/cloud_trace.h"
#include "contrib/endpoints/src/api_manager/context/service_context.h"
#include "contrib/endpoints/src/api_manager/service_control/info.h"

namespace google {
namespace api_manager {
namespace context {

// Stores request related data to be used by CheckHandler.
class RequestContext {
 public:
  RequestContext(std::shared_ptr<context::ServiceContext> service_context,
                 std::unique_ptr<Request> request);

  // Get the ApiManagerImpl object.
  context::ServiceContext *service_context() const {
    return service_context_.get();
  }

  // Get the request object.
  Request *request() const { return request_.get(); }

  // Get the method info.
  const MethodInfo *method() const { return method_call_.method_info; }

  // Get the method info.
  const MethodCallInfo *method_call() const { return &method_call_; }

  // Get the api key.
  const std::string &api_key() const { return api_key_; }

  // set the final check continuation callback function.
  void set_check_continuation(
      std::function<void(utils::Status status)> continuation) {
    check_continuation_ = continuation;
  }

  // set the is_api_key_valid field.
  void set_check_response_info(
      const service_control::CheckResponseInfo &check_response_info) {
    check_response_info_ = check_response_info;
  }

  // Fill CheckRequestInfo
  void FillCheckRequestInfo(service_control::CheckRequestInfo *info);

  // FillAllocateQuotaRequestInfo
  void FillAllocateQuotaRequestInfo(service_control::QuotaRequestInfo *info);

  // Fill ReportRequestInfo
  void FillReportRequestInfo(Response *response,
                             service_control::ReportRequestInfo *info);

  // Complete check.
  void CompleteCheck(utils::Status status);

  // Sets auth issuer to request context.
  void set_auth_issuer(const std::string &issuer) { auth_issuer_ = issuer; }

  // Sets auth audience to request context.
  void set_auth_audience(const std::string &audience) {
    auth_audience_ = audience;
  }

  // Sets authorized party to request context. The authorized party is read
  // from the "azp" claim in the auth token.
  void set_auth_authorized_party(const std::string &authorized_party) {
    auth_authorized_party_ = authorized_party;
  }

  // Get CloudTrace object.
  cloud_trace::CloudTrace *cloud_trace() { return cloud_trace_.get(); }

  // Marks the start of backend trace span, set the trace context header to
  // backend.
  void StartBackendSpanAndSetTraceContext();

  // Marks the end of backend trace span.
  void EndBackendSpan() { backend_span_.reset(); }

  // To indicate if the next report is the first_report or not.
  bool is_first_report() const { return is_first_report_; }
  void set_first_report(bool is_first_report) {
    is_first_report_ = is_first_report;
  }

  // Get the last intermediate report time point.
  std::chrono::steady_clock::time_point last_report_time() const {
    return last_report_time_;
  }
  // Set the last intermediate report time point.
  void set_last_report_time(std::chrono::steady_clock::time_point tp) {
    last_report_time_ = tp;
  }

  // Get the HTTP method to be used for the request. This method understands the
  // X-Http-Method-Override header and if present, returns the
  // X-Http-Method-Override method. Otherwise, the actual HTTP method is
  // returned.
  std::string GetRequestHTTPMethodWithOverride();

  // Set the auth claims from JWT token
  void set_auth_claims(const std::string &claims) { auth_claims_ = claims; }

  // Get the auth claims.
  const std::string &auth_claims() const { return auth_claims_; }

 private:
  // Fill OperationInfo
  void FillOperationInfo(service_control::OperationInfo *info);

  // Fill location info.
  void FillLocation(service_control::ReportRequestInfo *info);

  // Fill compute platform information.
  void FillComputePlatform(service_control::ReportRequestInfo *info);

  // Fill log message.
  void FillLogMessage(service_control::ReportRequestInfo *info);

  // Extracts api-key
  void ExtractApiKey();

  // The ApiManagerImpl object.
  std::shared_ptr<context::ServiceContext> service_context_;

  // request object to encapsulate request data.
  std::unique_ptr<Request> request_;

  // The final check continuation
  std::function<void(utils::Status status)> check_continuation_;

  // The method info from service config.
  MethodCallInfo method_call_;

  // Randomly generated UUID for each request, passed to service control
  // Check and Report calls.
  std::string operation_id_;

  // api key.
  std::string api_key_;

  // Pass check response data to Report call.
  service_control::CheckResponseInfo check_response_info_;

  // Needed by both Check() and Report, extract it once and store it here.
  std::string http_referer_;

  // auth_issuer. It will be used in service control Report().
  std::string auth_issuer_;

  // auth_audience. It will be used in service control Report().
  std::string auth_audience_;

  // auth_authorized_party. It will be used in service control Check() and
  // Report().
  std::string auth_authorized_party_;

  // Auth Claims: This is the decoded payload of the JWT token
  std::string auth_claims_;

  // Used by cloud tracing.
  std::unique_ptr<cloud_trace::CloudTrace> cloud_trace_;

  // Backend trace span.
  std::shared_ptr<cloud_trace::CloudTraceSpan> backend_span_;

  // Start time of the request_context instantiation.
  std::chrono::system_clock::time_point start_time_;

  // Flag to indicate the first report.
  bool is_first_report_;

  // The time point of last intermediate report
  std::chrono::steady_clock::time_point last_report_time_;

  // The accumulated data sent till last intermediate report
  int64_t last_request_bytes_;
  int64_t last_response_bytes_;
};

}  // namespace context
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_CONTEXT_REQUEST_CONTEXT_H_
