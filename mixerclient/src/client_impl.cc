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

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

MixerClientImpl::MixerClientImpl(const MixerClientOptions &options)
    : options_(options) {
  check_cache_ =
      std::unique_ptr<CheckCache>(new CheckCache(options.check_options));
  report_batch_ = std::unique_ptr<ReportBatch>(
      new ReportBatch(options.report_options, options_.report_transport,
                      options.timer_create_func, compressor_));
  quota_cache_ =
      std::unique_ptr<QuotaCache>(new QuotaCache(options.quota_options));

  if (options_.uuid_generate_func) {
    deduplication_id_base_ = options_.uuid_generate_func();
  }
}

MixerClientImpl::~MixerClientImpl() {}

CancelFunc MixerClientImpl::Check(const Attributes &attributes,
                                  TransportCheckFunc transport,
                                  DoneFunc on_done) {
  std::unique_ptr<CheckCache::CheckResult> check_result(
      new CheckCache::CheckResult);
  check_cache_->Check(attributes, check_result.get());
  if (check_result->IsCacheHit() && !check_result->status().ok()) {
    on_done(check_result->status());
    return nullptr;
  }

  std::unique_ptr<QuotaCache::CheckResult> quota_result(
      new QuotaCache::CheckResult);
  // Only use quota cache if Check is using cache with OK status.
  // Otherwise, a remote Check call may be rejected, but quota amounts were
  // substracted from quota cache already.
  quota_cache_->Check(attributes, check_result->IsCacheHit(),
                      quota_result.get());

  CheckRequest request;
  bool quota_call = quota_result->BuildRequest(&request);
  if (check_result->IsCacheHit() && quota_result->IsCacheHit()) {
    on_done(quota_result->status());
    on_done = nullptr;
    if (!quota_call) {
      return nullptr;
    }
  }

  compressor_.Compress(attributes, request.mutable_attributes());
  request.set_global_word_count(compressor_.global_word_count());
  request.set_deduplication_id(deduplication_id_base_ +
                               std::to_string(deduplication_id_.fetch_add(1)));

  // Need to make a copy for processing the response for check cache.
  Attributes *request_copy = new Attributes(attributes);
  auto response = new CheckResponse;
  // Lambda capture could not pass unique_ptr, use raw pointer.
  CheckCache::CheckResult *raw_check_result = check_result.release();
  QuotaCache::CheckResult *raw_quota_result = quota_result.release();
  if (!transport) {
    transport = options_.check_transport;
  }
  return transport(
      request, response, [this, request_copy, response, raw_check_result,
                          raw_quota_result, on_done](const Status &status) {
        raw_check_result->SetResponse(status, *request_copy, *response);
        raw_quota_result->SetResponse(status, *request_copy, *response);
        if (on_done) {
          if (!raw_check_result->status().ok()) {
            on_done(raw_check_result->status());
          } else {
            on_done(raw_quota_result->status());
          }
        }
        delete raw_check_result;
        delete raw_quota_result;
        delete request_copy;
        delete response;

        if (InvalidDictionaryStatus(status)) {
          compressor_.ShrinkGlobalDictionary();
        }
      });
}

void MixerClientImpl::Report(const Attributes &attributes) {
  report_batch_->Report(attributes);
}

// Creates a MixerClient object.
std::unique_ptr<MixerClient> CreateMixerClient(
    const MixerClientOptions &options) {
  return std::unique_ptr<MixerClient>(new MixerClientImpl(options));
}

}  // namespace mixer_client
}  // namespace istio
