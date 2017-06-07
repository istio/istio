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
#include "src/client_impl.h"
#include "utils/protobuf.h"

using namespace std::chrono;
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
    : options_(options), converter_({}) {
  check_cache_ =
      std::unique_ptr<CheckCache>(new CheckCache(options.check_options));
  report_batch_ = std::unique_ptr<ReportBatch>(
      new ReportBatch(options.report_options, options_.report_transport,
                      options.timer_create_func, converter_));
  quota_cache_ = std::unique_ptr<QuotaCache>(new QuotaCache(
      options.quota_options, options_.quota_transport, converter_));
}

MixerClientImpl::~MixerClientImpl() {}

void MixerClientImpl::Check(const Attributes &attributes, DoneFunc on_done) {
  std::string signature;
  Status status =
      check_cache_->Check(attributes, system_clock::now(), &signature);
  if (status.error_code() != Code::NOT_FOUND) {
    on_done(status);
    return;
  }

  CheckRequest request;
  converter_.Convert(attributes, request.mutable_attributes());
  auto response = new CheckResponse;
  options_.check_transport(request, response, [this, signature, response,
                                               on_done](const Status &status) {
    if (!status.ok()) {
      if (options_.check_options.network_fail_open) {
        on_done(Status::OK);
      } else {
        on_done(status);
      }
    } else {
      on_done(check_cache_->CacheResponse(signature, *response,
                                          system_clock::now()));
    }
    delete response;
  });
}

void MixerClientImpl::Report(const Attributes &attributes) {
  report_batch_->Report(attributes);
}

void MixerClientImpl::Quota(const Attributes &attributes, DoneFunc on_done) {
  quota_cache_->Quota(attributes, on_done);
}

// Creates a MixerClient object.
std::unique_ptr<MixerClient> CreateMixerClient(
    const MixerClientOptions &options) {
  return std::unique_ptr<MixerClient>(new MixerClientImpl(options));
}

}  // namespace mixer_client
}  // namespace istio
