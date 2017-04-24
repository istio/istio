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

using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportResponse;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

MixerClientImpl::MixerClientImpl(const MixerClientOptions &options)
    : options_(options) {
  TransportInterface *transport = options_.transport;
  if (transport == nullptr) {
    GOOGLE_CHECK(!options_.mixer_server.empty());
    grpc_transport_.reset(new GrpcTransport(options_.mixer_server));
    transport = grpc_transport_.get();
  }
  check_transport_.reset(new CheckTransport(transport));
  report_transport_.reset(new ReportTransport(transport));
  quota_transport_.reset(new QuotaTransport(transport));

  check_cache_ =
      std::unique_ptr<CheckCache>(new CheckCache(options.check_options));
  quota_cache_ = std::unique_ptr<QuotaCache>(
      new QuotaCache(options.quota_options, quota_transport_.get()));
}

MixerClientImpl::~MixerClientImpl() { check_cache_->FlushAll(); }

void MixerClientImpl::Check(const Attributes &attributes, DoneFunc on_done) {
  auto response = new CheckResponse;
  std::string signature;
  Status status = check_cache_->Check(attributes, response, &signature);
  if (status.error_code() == Code::NOT_FOUND) {
    std::shared_ptr<CheckCache> check_cache_copy = check_cache_;
    check_transport_->Send(
        attributes, response,
        [check_cache_copy, signature, response, on_done](const Status &status) {
          if (status.ok()) {
            check_cache_copy->CacheResponse(signature, *response);
            on_done(ConvertRpcStatus(response->result()));
          } else {
            on_done(status);
          }
          delete response;
        });
    return;
  }

  if (status.ok()) {
    on_done(ConvertRpcStatus(response->result()));
  } else {
    on_done(status);
  }
  delete response;
}

void MixerClientImpl::Report(const Attributes &attributes, DoneFunc on_done) {
  auto response = new ReportResponse;
  report_transport_->Send(attributes, response,
                          [response, on_done](const Status &status) {
                            if (status.ok()) {
                              on_done(ConvertRpcStatus(response->result()));
                            } else {
                              on_done(status);
                            }
                            delete response;
                          });
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
