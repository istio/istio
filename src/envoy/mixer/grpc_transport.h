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

#include <memory>

#include "common/common/logger.h"
#include "envoy/event/dispatcher.h"
#include "envoy/grpc/rpc_channel.h"

#include "envoy/upstream/cluster_manager.h"
#include "include/client.h"

namespace Envoy {
namespace Http {
namespace Mixer {

// An object to use Envoy async_client to make grpc call.
class GrpcTransport : public Grpc::RpcChannelCallbacks,
                      public Logger::Loggable<Logger::Id::http> {
 public:
  GrpcTransport(Upstream::ClusterManager& cm);

  void onPreRequestCustomizeHeaders(Http::HeaderMap& headers) override {}

  void onSuccess() override;

  void onFailure(const Optional<uint64_t>& grpc_status,
                 const std::string& message) override;

  // Check if mixer server cluster configured in cluster_manager.
  static bool IsMixerServerConfigured(Upstream::ClusterManager& cm);

 protected:
  // Create a new grpc channel.
  Grpc::RpcChannelPtr NewChannel(Upstream::ClusterManager& cm);

  // The on_done callback function.
  ::istio::mixer_client::DoneFunc on_done_;
  // the grpc channel.
  Grpc::RpcChannelPtr channel_;
  // The generated mixer client stub.
  ::istio::mixer::v1::Mixer::Stub stub_;
};

// A helper class to pass Upstream::ClusterManager& in the lambda capture
// If passing Upstream::ClusterManager& directly in the capture, it assumes
// const Upstream::ClusterManager&.
class ClusterManagerStore {
 public:
  ClusterManagerStore(Upstream::ClusterManager& cm) : cm_(cm) {}
  Upstream::ClusterManager& cm() { return cm_; }

 private:
  Upstream::ClusterManager& cm_;
};

class CheckGrpcTransport : public GrpcTransport {
 public:
  CheckGrpcTransport(Upstream::ClusterManager& cm) : GrpcTransport(cm) {}

  static ::istio::mixer_client::TransportCheckFunc GetFunc(
      std::shared_ptr<ClusterManagerStore> cms) {
    return [cms](const ::istio::mixer::v1::CheckRequest& request,
                 ::istio::mixer::v1::CheckResponse* response,
                 ::istio::mixer_client::DoneFunc on_done) {
      CheckGrpcTransport* transport = new CheckGrpcTransport(cms->cm());
      transport->Call(request, response, on_done);
    };
  }
  void Call(const ::istio::mixer::v1::CheckRequest& request,
            ::istio::mixer::v1::CheckResponse* response,
            ::istio::mixer_client::DoneFunc on_done) {
    on_done_ = [this, response,
                on_done](const ::google::protobuf::util::Status& status) {
      if (status.ok()) {
        log().debug("Check response: {}", response->DebugString());
      }
      on_done(status);
    };
    log().debug("Call grpc check: {}", request.DebugString());
    stub_.Check(nullptr, &request, response, nullptr);
  }
};

class ReportGrpcTransport : public GrpcTransport {
 public:
  ReportGrpcTransport(Upstream::ClusterManager& cm) : GrpcTransport(cm) {}

  static ::istio::mixer_client::TransportReportFunc GetFunc(
      std::shared_ptr<ClusterManagerStore> cms) {
    return [cms](const ::istio::mixer::v1::ReportRequest& request,
                 ::istio::mixer::v1::ReportResponse* response,
                 ::istio::mixer_client::DoneFunc on_done) {
      ReportGrpcTransport* transport = new ReportGrpcTransport(cms->cm());
      transport->Call(request, response, on_done);
    };
  }
  void Call(const ::istio::mixer::v1::ReportRequest& request,
            ::istio::mixer::v1::ReportResponse* response,
            ::istio::mixer_client::DoneFunc on_done) {
    on_done_ = on_done;
    log().debug("Call grpc report: {}", request.DebugString());
    stub_.Report(nullptr, &request, response, nullptr);
  }
};

class QuotaGrpcTransport : public GrpcTransport {
 public:
  QuotaGrpcTransport(Upstream::ClusterManager& cm) : GrpcTransport(cm) {}

  static ::istio::mixer_client::TransportQuotaFunc GetFunc(
      std::shared_ptr<ClusterManagerStore> cms) {
    return [cms](const ::istio::mixer::v1::QuotaRequest& request,
                 ::istio::mixer::v1::QuotaResponse* response,
                 ::istio::mixer_client::DoneFunc on_done) {
      QuotaGrpcTransport* transport = new QuotaGrpcTransport(cms->cm());
      transport->Call(request, response, on_done);
    };
  }
  void Call(const ::istio::mixer::v1::QuotaRequest& request,
            ::istio::mixer::v1::QuotaResponse* response,
            ::istio::mixer_client::DoneFunc on_done) {
    on_done_ = [this, response,
                on_done](const ::google::protobuf::util::Status& status) {
      if (status.ok()) {
        log().debug("Quota response: {}", response->DebugString());
      }
      on_done(status);
    };
    log().debug("Call grpc quota: {}", request.DebugString());
    stub_.Quota(nullptr, &request, response, nullptr);
  }
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
