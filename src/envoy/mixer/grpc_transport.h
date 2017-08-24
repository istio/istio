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
#include "envoy/grpc/rpc_channel.h"

#include "envoy/upstream/cluster_manager.h"
#include "include/client.h"

namespace Envoy {
namespace Http {
namespace Mixer {

// An object to use Envoy::Grpc::AsyncClient to make grpc call.
template <class RequestType, class ResponseType>
class GrpcTransport : public Grpc::AsyncRequestCallbacks<ResponseType>,
                      public Logger::Loggable<Logger::Id::http> {
 public:
  using Func =
      std::function<void(const RequestType& request, ResponseType* response,
                         istio::mixer_client::DoneFunc on_done)>;

  using AsyncClient = Grpc::AsyncClient<RequestType, ResponseType>;

  typedef std::unique_ptr<AsyncClient> AsyncClientPtr;

  static Func GetFunc(Upstream::ClusterManager& cm,
                      const HeaderMap* headers = nullptr);

  GrpcTransport(AsyncClientPtr async_client, const RequestType& request,
                const HeaderMap* headers, ResponseType* response,
                istio::mixer_client::DoneFunc on_done);

  // Grpc::AsyncRequestCallbacks<ResponseType>
  void onCreateInitialMetadata(Http::HeaderMap& metadata) override;

  void onSuccess(std::unique_ptr<ResponseType>&& response) override;

  void onFailure(Grpc::Status::GrpcStatus status,
                 const std::string& message) override;

 private:
  static const google::protobuf::MethodDescriptor& descriptor();

  AsyncClientPtr async_client_;
  const HeaderMap* headers_;
  ResponseType* response_;
  ::istio::mixer_client::DoneFunc on_done_;
  Grpc::AsyncRequest* request_{};
};

typedef GrpcTransport<istio::mixer::v1::CheckRequest,
                      istio::mixer::v1::CheckResponse>
    CheckTransport;

typedef GrpcTransport<istio::mixer::v1::ReportRequest,
                      istio::mixer::v1::ReportResponse>
    ReportTransport;

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
