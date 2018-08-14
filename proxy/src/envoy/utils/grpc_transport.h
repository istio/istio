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

#pragma once

#include <common/grpc/async_client_impl.h>
#include <memory>

#include "common/common/logger.h"
#include "envoy/event/dispatcher.h"
#include "envoy/grpc/async_client.h"
#include "envoy/http/header_map.h"

#include "envoy/upstream/cluster_manager.h"
#include "proxy/include/istio/mixerclient/client.h"

namespace Envoy {
namespace Utils {

// An object to use Envoy::Grpc::AsyncClient to make grpc call.
template <class RequestType, class ResponseType>
class GrpcTransport : public Grpc::TypedAsyncRequestCallbacks<ResponseType>,
                      public Logger::Loggable<Logger::Id::grpc> {
 public:
  using Func = std::function<istio::mixerclient::CancelFunc(
      const RequestType& request, ResponseType* response,
      istio::mixerclient::DoneFunc on_done)>;

  static Func GetFunc(Grpc::AsyncClientFactory& factory,
                      Tracing::Span& parent_span,
                      const std::string& serialized_forward_attributes);

  GrpcTransport(Grpc::AsyncClientPtr async_client, const RequestType& request,
                ResponseType* response, Tracing::Span& parent_span,
                const std::string& serialized_forward_attributes,
                istio::mixerclient::DoneFunc on_done);

  void onCreateInitialMetadata(Http::HeaderMap& metadata) override;

  void onSuccess(std::unique_ptr<ResponseType>&& response,
                 Tracing::Span& span) override;

  void onFailure(Grpc::Status::GrpcStatus status, const std::string& message,
                 Tracing::Span& span) override;

  void Cancel();

 private:
  static const google::protobuf::MethodDescriptor& descriptor();

  Grpc::AsyncClientPtr async_client_;
  ResponseType* response_;
  const std::string& serialized_forward_attributes_;
  ::istio::mixerclient::DoneFunc on_done_;
  Grpc::AsyncRequest* request_{};
};

typedef GrpcTransport<istio::mixer::v1::CheckRequest,
                      istio::mixer::v1::CheckResponse>
    CheckTransport;

typedef GrpcTransport<istio::mixer::v1::ReportRequest,
                      istio::mixer::v1::ReportResponse>
    ReportTransport;

}  // namespace Utils
}  // namespace Envoy
