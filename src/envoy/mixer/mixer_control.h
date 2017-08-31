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
#include <mutex>
#include <thread>
#include <unordered_map>

#include "common/common/logger.h"
#include "common/http/headers.h"
#include "envoy/grpc/async_client.h"
#include "envoy/http/access_log.h"
#include "envoy/thread_local/thread_local.h"
#include "envoy/upstream/cluster_manager.h"
#include "include/client.h"
#include "src/envoy/mixer/config.h"
#include "src/envoy/mixer/grpc_transport.h"
#include "src/envoy/mixer/string_map.pb.h"
#include "src/envoy/mixer/utils.h"

namespace Envoy {
namespace Http {
namespace Mixer {

// Store data from Check to report
struct HttpRequestData {
  ::istio::mixer_client::Attributes attributes;
};
typedef std::shared_ptr<HttpRequestData> HttpRequestDataPtr;

// The mixer client class to control HTTP requests.
// It has Check() to validate if a request can be processed.
// At the end of request, call Report().
class MixerControl final : public ThreadLocal::ThreadLocalObject,
                           public Logger::Loggable<Logger::Id::http> {
 public:
  // The constructor.
  MixerControl(const MixerConfig& mixer_config, Upstream::ClusterManager& cm,
               Event::Dispatcher& dispatcher, Runtime::RandomGenerator& random);

  // Add a special header to forward mixer attribues to upstream proxy.
  void ForwardAttributes(HeaderMap& headers,
                         const Utils::StringMap& route_attributes) const;

  // Build check request attributes for HTTP.
  void BuildHttpCheck(HttpRequestDataPtr request_data, HeaderMap& headers,
                      const ::istio::proxy::mixer::StringMap& map_pb,
                      const std::string& source_user,
                      const Utils::StringMap& route_attributes,
                      const Network::Connection* connection) const;

  // Build report request attributs for HTTP.
  void BuildHttpReport(HttpRequestDataPtr request_data,
                       const HeaderMap* response_headers,
                       const AccessLog::RequestInfo& request_info,
                       int check_status_code) const;

  // Build check request attributes for Tcp.
  void BuildTcpCheck(HttpRequestDataPtr request_data,
                     Network::Connection& connection,
                     const std::string& source_user) const;

  // Build report request attributs for Tcp.
  void BuildTcpReport(
      HttpRequestDataPtr request_data, uint64_t received_bytes,
      uint64_t send_bytes, int check_status_code,
      std::chrono::nanoseconds duration,
      Upstream::HostDescriptionConstSharedPtr upstreamHost) const;

  // Make remote check call.
  istio::mixer_client::CancelFunc SendCheck(
      HttpRequestDataPtr request_data, const HeaderMap* headers,
      ::istio::mixer_client::DoneFunc on_done);

  // Make remote report call.
  void SendReport(HttpRequestDataPtr request_data);

  // See if check calls are disabled for Tcp proxy
  bool MixerTcpCheckDisabled() const {
    return mixer_config_.disable_tcp_check_calls;
  }

 private:
  // Envoy cluster manager for making gRPC calls.
  Upstream::ClusterManager& cm_;
  // The mixer client
  std::unique_ptr<::istio::mixer_client::MixerClient> mixer_client_;
  // The mixer config
  const MixerConfig& mixer_config_;
  // Quota attributes; extracted from envoy filter config.
  ::istio::mixer_client::Attributes quota_attributes_;

  CheckTransport::AsyncClientPtr check_client_;
  ReportTransport::AsyncClientPtr report_client_;
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
