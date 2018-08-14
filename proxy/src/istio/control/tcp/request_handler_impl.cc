/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#include "src/istio/control/tcp/request_handler_impl.h"
#include "src/istio/control/tcp/attributes_builder.h"

using ::google::protobuf::util::Status;
using ::istio::mixerclient::CancelFunc;
using ::istio::mixerclient::CheckDoneFunc;
using ::istio::mixerclient::CheckResponseInfo;
using ::istio::quota_config::Requirement;

namespace istio {
namespace control {
namespace tcp {

RequestHandlerImpl::RequestHandlerImpl(
    std::shared_ptr<ClientContext> client_context)
    : client_context_(client_context),
      last_report_info_{0ULL, 0ULL, std::chrono::nanoseconds::zero()} {}

CancelFunc RequestHandlerImpl::Check(CheckData* check_data,
                                     CheckDoneFunc on_done) {
  if (client_context_->enable_mixer_check() ||
      client_context_->enable_mixer_report()) {
    client_context_->AddStaticAttributes(&request_context_);

    AttributesBuilder builder(&request_context_);
    builder.ExtractCheckAttributes(check_data);
  }

  if (!client_context_->enable_mixer_check()) {
    CheckResponseInfo check_response_info;
    check_response_info.response_status = Status::OK;
    on_done(check_response_info);
    return nullptr;
  }

  client_context_->AddQuotas(&request_context_);

  return client_context_->SendCheck(nullptr, on_done, &request_context_);
}

void RequestHandlerImpl::Report(ReportData* report_data,
                                ReportData::ConnectionEvent event) {
  if (!client_context_->enable_mixer_report()) {
    return;
  }

  AttributesBuilder builder(&request_context_);
  builder.ExtractReportAttributes(report_data, event, &last_report_info_);

  client_context_->SendReport(request_context_);
}

}  // namespace tcp
}  // namespace control
}  // namespace istio
