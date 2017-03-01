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
#include <sstream>

using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportResponse;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {
namespace {
Status ConvertRpcStatus(const ::google::rpc::Status &status) {
  return Status(static_cast<Code>(status.code()), status.message());
}
}  // namespace

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
}

MixerClientImpl::~MixerClientImpl() {}

void MixerClientImpl::Check(const Attributes &attributes, DoneFunc on_done) {
  auto response = new CheckResponse;
  check_transport_->Send(attributes, response,
                         [response, on_done](const Status &status) {
                           if (status.ok()) {
                             on_done(ConvertRpcStatus(response->result()));
                           } else {
                             on_done(status);
                           }
                           delete response;
                         });
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
  auto response = new QuotaResponse;
  quota_transport_->Send(attributes, response,
                         [response, on_done](const Status &status) {
                           if (status.ok()) {
                             on_done(ConvertRpcStatus(response->result()));
                           } else {
                             on_done(status);
                           }
                           delete response;
                         });
}

// Creates a MixerClient object.
std::unique_ptr<MixerClient> CreateMixerClient(
    const MixerClientOptions &options) {
  return std::unique_ptr<MixerClient>(new MixerClientImpl(options));
}

std::string Attributes::DebugString() const {
  std::stringstream ss;
  for (const auto &it : attributes) {
    ss << it.first << ": ";
    switch (it.second.type) {
      case Attributes::Value::ValueType::STRING:
        ss << "(STRING): " << it.second.str_v;
        break;
      case Attributes::Value::ValueType::BYTES:
        ss << "(BYTES): " << it.second.str_v;
        break;
      case Attributes::Value::ValueType::INT64:
        ss << "(INT64): " << it.second.value.int64_v;
        break;
      case Attributes::Value::ValueType::DOUBLE:
        ss << "(DOUBLE): " << it.second.value.double_v;
        break;
      case Attributes::Value::ValueType::BOOL:
        ss << "(BOOL): " << it.second.value.bool_v;
        break;
      case Attributes::Value::ValueType::TIME:
        ss << "(TIME ms): "
           << std::chrono::duration_cast<std::chrono::microseconds>(
                  it.second.time_v.time_since_epoch())
                  .count();
        break;
      case Attributes::Value::ValueType::DURATION:
        ss << "(DURATION nanos): " << it.second.duration_nanos_v.count();
        break;
      case Attributes::Value::ValueType::STRING_MAP:
        ss << "(STRING MAP):";
        for (const auto &map_it : it.second.string_map_v) {
          ss << std::endl;
          ss << "      " << map_it.first << ": " << map_it.second;
        }
        break;
    }
    ss << std::endl;
  }
  return ss.str();
}

}  // namespace mixer_client
}  // namespace istio
