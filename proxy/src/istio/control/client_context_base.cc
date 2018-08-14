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

#include "client_context_base.h"
#include "include/istio/mixerclient/check_response.h"
#include "include/istio/utils/attribute_names.h"
#include "include/istio/utils/attributes_builder.h"

using ::google::protobuf::util::Status;
using ::istio::mixer::v1::config::client::NetworkFailPolicy;
using ::istio::mixer::v1::config::client::TransportConfig;
using ::istio::mixerclient::CancelFunc;
using ::istio::mixerclient::CheckDoneFunc;
using ::istio::mixerclient::CheckOptions;
using ::istio::mixerclient::CheckResponseInfo;
using ::istio::mixerclient::Environment;
using ::istio::mixerclient::MixerClientOptions;
using ::istio::mixerclient::QuotaOptions;
using ::istio::mixerclient::ReportOptions;
using ::istio::mixerclient::Statistics;
using ::istio::mixerclient::TransportCheckFunc;

namespace istio {
namespace control {
namespace {

CheckOptions GetJustCheckOptions(const TransportConfig& config) {
  if (config.disable_check_cache()) {
    return CheckOptions(0);
  }
  return CheckOptions();
}

CheckOptions GetCheckOptions(const TransportConfig& config) {
  auto options = GetJustCheckOptions(config);
  if (config.has_network_fail_policy() &&
      config.network_fail_policy().policy() == NetworkFailPolicy::FAIL_CLOSE) {
    options.network_fail_open = false;
  }
  return options;
}

QuotaOptions GetQuotaOptions(const TransportConfig& config) {
  if (config.disable_quota_cache()) {
    return QuotaOptions(0, 1000);
  }
  return QuotaOptions();
}

ReportOptions GetReportOptions(const TransportConfig& config) {
  if (config.disable_report_batch()) {
    return ReportOptions(0, 1000);
  }
  return ReportOptions();
}

}  // namespace

ClientContextBase::ClientContextBase(const TransportConfig& config,
                                     const Environment& env) {
  MixerClientOptions options(GetCheckOptions(config), GetReportOptions(config),
                             GetQuotaOptions(config));
  options.env = env;
  mixer_client_ = ::istio::mixerclient::CreateMixerClient(options);
}

CancelFunc ClientContextBase::SendCheck(TransportCheckFunc transport,
                                        CheckDoneFunc on_done,
                                        RequestContext* request) {
  // Intercept the callback to save check status in request_context
  auto local_on_done = [request,
                        on_done](const CheckResponseInfo& check_response_info) {
    // save the check status code
    request->check_status = check_response_info.response_status;

    utils::AttributesBuilder builder(&request->attributes);
    builder.AddBool(utils::AttributeName::kCheckCacheHit,
                    check_response_info.is_check_cache_hit);
    builder.AddBool(utils::AttributeName::kQuotaCacheHit,
                    check_response_info.is_quota_cache_hit);
    on_done(check_response_info);
  };

  // TODO: add debug message
  // GOOGLE_LOG(INFO) << "Check attributes: " <<
  // request->attributes.DebugString();
  return mixer_client_->Check(request->attributes, request->quotas, transport,
                              local_on_done);
}

void ClientContextBase::SendReport(const RequestContext& request) {
  // TODO: add debug message
  // GOOGLE_LOG(INFO) << "Report attributes: " <<
  // request.attributes.DebugString();
  mixer_client_->Report(request.attributes);
}

void ClientContextBase::GetStatistics(Statistics* stat) const {
  mixer_client_->GetStatistics(stat);
}

}  // namespace control
}  // namespace istio
