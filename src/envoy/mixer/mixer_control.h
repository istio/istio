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

#include "control/include/http/controller.h"
#include "control/include/tcp/controller.h"
#include "envoy/event/dispatcher.h"
#include "envoy/runtime/runtime.h"
#include "envoy/thread_local/thread_local.h"
#include "envoy/upstream/cluster_manager.h"
#include "src/envoy/mixer/config.h"

namespace Envoy {
namespace Http {
namespace Mixer {

class HttpMixerControl final : public ThreadLocal::ThreadLocalObject {
 public:
  // The constructor.
  HttpMixerControl(const HttpMixerConfig& mixer_config,
                   Upstream::ClusterManager& cm, Event::Dispatcher& dispatcher,
                   Runtime::RandomGenerator& random);

  Upstream::ClusterManager& cm() { return cm_; }

  const std::string& check_cluster() const { return check_cluster_; }

  const std::string& report_cluster() const { return report_cluster_; }

  ::istio::mixer_control::http::Controller* controller() {
    return controller_.get();
  }

  bool has_v2_config() const { return has_v2_config_; }

 private:
  // Envoy cluster manager for making gRPC calls.
  Upstream::ClusterManager& cm_;
  // The mixer control
  std::unique_ptr<::istio::mixer_control::http::Controller> controller_;
  // has v2 config;
  bool has_v2_config_;
  // check cluster
  std::string check_cluster_;
  // report cluster
  std::string report_cluster_;
};

class TcpMixerControl final : public ThreadLocal::ThreadLocalObject {
 public:
  // The constructor.
  TcpMixerControl(const TcpMixerConfig& mixer_config,
                  Upstream::ClusterManager& cm, Event::Dispatcher& dispatcher,
                  Runtime::RandomGenerator& random);

  ::istio::mixer_control::tcp::Controller* controller() {
    return controller_.get();
  }

  std::chrono::milliseconds report_interval_ms() const {
    return report_interval_ms_;
  }

  const std::string& check_cluster() const { return check_cluster_; }

  const std::string& report_cluster() const { return report_cluster_; }

  Event::Dispatcher& dispatcher() { return dispatcher_; }

 private:
  // The mixer control
  std::unique_ptr<::istio::mixer_control::tcp::Controller> controller_;

  // Time interval in milliseconds for sending periodical delta reports.
  std::chrono::milliseconds report_interval_ms_;

  Event::Dispatcher& dispatcher_;

  // check cluster
  std::string check_cluster_;
  // report cluster
  std::string report_cluster_;
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
