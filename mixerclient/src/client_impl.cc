/* Copyright 2017 Google Inc. All Rights Reserved.
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
#include "src/client_impl.h"
#include "mixer/api/v1/service.pb.h"

using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::istio::mixer::v1::QuotaRequest;
using ::istio::mixer::v1::QuotaResponse;

using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

MixerClientImpl::MixerClientImpl(MixerClientOptions &options)
    : options_(options) {}

MixerClientImpl::~MixerClientImpl() {}

void MixerClientImpl::Check(const CheckRequest &check_request,
                            CheckResponse *check_response,
                            DoneFunc on_check_done) {
  if (options_.check_transport == NULL) {
    on_check_done(Status(Code::INVALID_ARGUMENT, "transport is NULL."));
    return;
  }

  options_.check_transport(check_request, check_response, on_check_done);
}

void MixerClientImpl::Report(const ReportRequest &report_request,
                             ReportResponse *report_response,
                             DoneFunc on_report_done) {
  if (options_.report_transport == NULL) {
    on_report_done(Status(Code::INVALID_ARGUMENT, "transport is NULL."));
    return;
  }

  options_.report_transport(report_request, report_response, on_report_done);
}

void MixerClientImpl::Quota(const QuotaRequest &quota_request,
                            QuotaResponse *quota_response,
                            DoneFunc on_quota_done) {
  if (options_.quota_transport == NULL) {
    on_quota_done(Status(Code::INVALID_ARGUMENT, "transport is NULL."));
    return;
  }

  options_.quota_transport(quota_request, quota_response, on_quota_done);
}

// Creates a MixerClient object.
std::unique_ptr<MixerClient> CreateMixerClient(MixerClientOptions &options) {
  return std::unique_ptr<MixerClient>(new MixerClientImpl(options));
}

}  // namespace mixer_client
}  // namespace istio
