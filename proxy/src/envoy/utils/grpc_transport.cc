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
#include "proxy/src/envoy/utils/grpc_transport.h"
#include "absl/types/optional.h"
#include "proxy/src/envoy/utils/header_update.h"

using ::google::protobuf::util::Status;
using StatusCode = ::google::protobuf::util::error::Code;
using ::istio::mixer::v1::Attributes;

namespace Envoy {
namespace Utils {
namespace {

// gRPC request timeout
const std::chrono::milliseconds kGrpcRequestTimeoutMs(5000);

}  // namespace

template <class RequestType, class ResponseType>
GrpcTransport<RequestType, ResponseType>::GrpcTransport(
    Grpc::AsyncClientPtr async_client, const RequestType &request,
    ResponseType *response, Tracing::Span &parent_span,
    const std::string &serialized_forward_attributes,
    istio::mixerclient::DoneFunc on_done)
    : async_client_(std::move(async_client)),
      response_(response),
      serialized_forward_attributes_(serialized_forward_attributes),
      on_done_(on_done),
      request_(async_client_->send(
          descriptor(), request, *this, parent_span,
          absl::optional<std::chrono::milliseconds>(kGrpcRequestTimeoutMs))) {
  ENVOY_LOG(debug, "Sending {} request: {}", descriptor().name(),
            request.DebugString());
}

template <class RequestType, class ResponseType>
void GrpcTransport<RequestType, ResponseType>::onCreateInitialMetadata(
    Http::HeaderMap &metadata) {
  // We generate cluster name contains invalid characters, so override the
  // authority header temorarily until it can be specified via CDS.
  // See https://github.com/envoyproxy/envoy/issues/3297 for details.
  metadata.Host()->value("mixer", 5);

  if (!serialized_forward_attributes_.empty()) {
    HeaderUpdate header_update_(&metadata);
    header_update_.AddIstioAttributes(serialized_forward_attributes_);
  }
}

template <class RequestType, class ResponseType>
void GrpcTransport<RequestType, ResponseType>::onSuccess(
    std::unique_ptr<ResponseType> &&response, Tracing::Span &) {
  ENVOY_LOG(debug, "{} response: {}", descriptor().name(),
            response->DebugString());
  response->Swap(response_);
  on_done_(Status::OK);
  delete this;
}

template <class RequestType, class ResponseType>
void GrpcTransport<RequestType, ResponseType>::onFailure(
    Grpc::Status::GrpcStatus status, const std::string &message,
    Tracing::Span &) {
  ENVOY_LOG(debug, "{} failed with code: {}, {}", descriptor().name(), status,
            message);
  on_done_(Status(static_cast<StatusCode>(status), message));
  delete this;
}

template <class RequestType, class ResponseType>
void GrpcTransport<RequestType, ResponseType>::Cancel() {
  ENVOY_LOG(debug, "Cancel gRPC request {}", descriptor().name());
  delete this;
}

template <class RequestType, class ResponseType>
typename GrpcTransport<RequestType, ResponseType>::Func
GrpcTransport<RequestType, ResponseType>::GetFunc(
    Grpc::AsyncClientFactory &factory, Tracing::Span &parent_span,
    const std::string &serialized_forward_attributes) {
  return [&factory, &parent_span, &serialized_forward_attributes](
             const RequestType &request, ResponseType *response,
             istio::mixerclient::DoneFunc on_done)
             -> istio::mixerclient::CancelFunc {
    auto transport = new GrpcTransport<RequestType, ResponseType>(
        factory.create(), request, response, parent_span,
        serialized_forward_attributes, on_done);
    return [transport]() { transport->Cancel(); };
  };
}

template <>
const google::protobuf::MethodDescriptor &CheckTransport::descriptor() {
  static const google::protobuf::MethodDescriptor *check_descriptor =
      istio::mixer::v1::Mixer::descriptor()->FindMethodByName("Check");
  ASSERT(check_descriptor);

  return *check_descriptor;
}

template <>
const google::protobuf::MethodDescriptor &ReportTransport::descriptor() {
  static const google::protobuf::MethodDescriptor *report_descriptor =
      istio::mixer::v1::Mixer::descriptor()->FindMethodByName("Report");
  ASSERT(report_descriptor);

  return *report_descriptor;
}

// explicitly instantiate CheckTransport and ReportTransport
template CheckTransport::Func CheckTransport::GetFunc(
    Grpc::AsyncClientFactory &factory, Tracing::Span &parent_span,
    const std::string &serialized_forward_attributes);
template ReportTransport::Func ReportTransport::GetFunc(
    Grpc::AsyncClientFactory &factory, Tracing::Span &parent_span,
    const std::string &serialized_forward_attributes);

}  // namespace Utils
}  // namespace Envoy
