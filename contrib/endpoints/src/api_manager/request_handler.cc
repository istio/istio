// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/api_manager/request_handler.h"

#include "contrib/endpoints/src/api_manager/auth/service_account_token.h"
#include "contrib/endpoints/src/api_manager/check_workflow.h"
#include "contrib/endpoints/src/api_manager/cloud_trace/cloud_trace.h"
#include "contrib/endpoints/src/api_manager/utils/marshalling.h"
#include "google/devtools/cloudtrace/v1/trace.pb.h"
#include "google/protobuf/stubs/logging.h"

using ::google::api_manager::utils::Status;
using google::devtools::cloudtrace::v1::Traces;

namespace google {
namespace api_manager {

void RequestHandler::Check(std::function<void(Status status)> continuation) {
  if (context_->method() && context_->method()->skip_service_control()) {
    continuation(Status::OK);
    return;
  }
  auto interception = [continuation, this](Status status) {
    if (status.ok() && context_->cloud_trace()) {
      context_->StartBackendSpanAndSetTraceContext();
    }
    continuation(status);
  };

  context_->set_check_continuation(interception);

  // Run the check flow.
  check_workflow_->Run(context_);
}

void RequestHandler::AttemptIntermediateReport() {
  // For grpc streaming calls, we send intermediate reports to represent
  // streaming stats. Specifically:
  // 1) We send request_count in the first report to indicate the start of a
  // stream.
  // 2) We send request_bytes, response_bytes in intermediate reports, which
  // triggered by timer.
  // 3) In the final report, we send all metrics except request_count if it
  // already sent.
  // We only send intermediate streaming report if the time_interval >
  // intermediate_report_interval().
  if (std::chrono::duration_cast<std::chrono::seconds>(
          std::chrono::steady_clock::now() - context_->last_report_time())
          .count() <
      context_->service_context()->intermediate_report_interval()) {
    return;
  }
  service_control::ReportRequestInfo info;
  info.is_first_report = context_->is_first_report();
  info.is_final_report = false;
  context_->FillReportRequestInfo(NULL, &info);

  // Calling service_control Report.
  Status status = context_->service_context()->service_control()->Report(info);
  if (!status.ok()) {
    context_->service_context()->env()->LogError(
        "Failed to send intermediate report to service control.");
  } else {
    context_->set_first_report(false);
  }
  context_->set_last_report_time(std::chrono::steady_clock::now());
}

// Sends a report.
void RequestHandler::Report(std::unique_ptr<Response> response,
                            std::function<void(void)> continuation) {
  if (context_->method() && context_->method()->skip_service_control()) {
    continuation();
    return;
  }
  // Close backend trace span.
  context_->EndBackendSpan();

  if (context_->service_context()->service_control()) {
    service_control::ReportRequestInfo info;
    info.is_first_report = context_->is_first_report();
    info.is_final_report = true;
    context_->FillReportRequestInfo(response.get(), &info);
    // Calling service_control Report.
    Status status =
        context_->service_context()->service_control()->Report(info);
    if (!status.ok()) {
      context_->service_context()->env()->LogError(
          "Failed to send report to service control.");
    }
  }

  if (context_->cloud_trace()) {
    context_->cloud_trace()->EndRootSpan();
    // Always set the project_id to the latest one.
    //
    // this is how project_id is calculated: if gce metadata is fetched, use
    // its project_id. Otherwise, use the project_id from service_config if it
    // is configured.
    // gce metadata fetching is started by the first request. While fetching is
    // in progress, subsequent requests will fail.  These failed requests may
    // have wrong project_id until gce metadata is fetched successfully.
    context_->service_context()->cloud_trace_aggregator()->SetProjectId(
        context_->service_context()->project_id());
    context_->service_context()->cloud_trace_aggregator()->AppendTrace(
        context_->cloud_trace()->ReleaseTrace());
  }

  continuation();
}

std::string RequestHandler::GetServiceConfigId() const {
  return context_->service_context()->service().id();
}

std::string RequestHandler::GetBackendAddress() const {
  if (context_->method()) {
    return context_->method()->backend_address();
  } else {
    return std::string();
  }
}

std::string RequestHandler::GetRpcMethodFullName() const {
  if (context_ && context_->method() &&
      !context_->method()->rpc_method_full_name().empty()) {
    return context_->method()->rpc_method_full_name();
  } else {
    return std::string();
  }
}

}  // namespace api_manager
}  // namespace google
