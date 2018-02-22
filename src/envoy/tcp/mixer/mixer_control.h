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

#include "envoy/event/dispatcher.h"
#include "envoy/runtime/runtime.h"
#include "envoy/thread_local/thread_local.h"
#include "envoy/upstream/cluster_manager.h"
#include "include/control/tcp/controller.h"
#include "src/envoy/tcp/mixer/config.h"
#include "src/envoy/utils/stats.h"

namespace Envoy {
namespace Tcp {
namespace Mixer {

class TcpMixerControl final : public ThreadLocal::ThreadLocalObject {
 public:
  // The constructor.
  TcpMixerControl(const TcpMixerConfig& mixer_config,
                  Upstream::ClusterManager& cm, Event::Dispatcher& dispatcher,
                  Runtime::RandomGenerator& random,
                  Utils::MixerFilterStats& stats);

  ::istio::control::tcp::Controller* controller() { return controller_.get(); }

  std::chrono::milliseconds report_interval_ms() const {
    return report_interval_ms_;
  }

  Event::Dispatcher& dispatcher() { return dispatcher_; }

 private:
  // The mixer config.
  const TcpMixerConfig& config_;
  // The mixer control
  std::unique_ptr<::istio::control::tcp::Controller> controller_;

  // Time interval in milliseconds for sending periodical delta reports.
  std::chrono::milliseconds report_interval_ms_;

  Event::Dispatcher& dispatcher_;

  Utils::MixerStatsObject stats_obj_;
};

}  // namespace Mixer
}  // namespace Tcp
}  // namespace Envoy
