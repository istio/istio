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

MixerClientImpl::MixerClientImpl(const MixerClientOptions &options)
    : options_(options),
      check_transport_(options_.transport),
      report_transport_(options_.transport),
      quota_transport_(options_.transport) {}

MixerClientImpl::~MixerClientImpl() {}

void MixerClientImpl::Check(const CheckRequest &request,
                            CheckResponse *response, DoneFunc on_done) {
  check_transport_.Call(request, response, on_done);
}

void MixerClientImpl::Report(const ReportRequest &request,
                             ReportResponse *response, DoneFunc on_done) {
  report_transport_.Call(request, response, on_done);
}

void MixerClientImpl::Quota(const QuotaRequest &request,
                            QuotaResponse *response, DoneFunc on_done) {
  quota_transport_.Call(request, response, on_done);
}

// Creates a MixerClient object.
std::unique_ptr<MixerClient> CreateMixerClient(MixerClientOptions &options) {
  return std::unique_ptr<MixerClient>(new MixerClientImpl(options));
}

}  // namespace mixer_client
}  // namespace istio
