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

#include "src/envoy/tcp/mixer/mixer_control.h"
#include "src/envoy/utils/mixer_control.h"

using ::istio::mixerclient::Statistics;

namespace Envoy {
namespace Tcp {
namespace Mixer {
namespace {

// Default time interval for periodical report is 10 seconds.
const std::chrono::milliseconds kDefaultReportIntervalMs(10000);

// Minimum time interval for periodical report is 1 seconds.
const std::chrono::milliseconds kMinReportIntervalMs(1000);

}  // namespace

TcpMixerControl::TcpMixerControl(const TcpMixerConfig& mixer_config,
                                 Upstream::ClusterManager& cm,
                                 Event::Dispatcher& dispatcher,
                                 Runtime::RandomGenerator& random,
                                 Utils::MixerFilterStats& stats)
    : config_(mixer_config),
      dispatcher_(dispatcher),
      stats_obj_(dispatcher, stats,
                 config_.tcp_config().transport().stats_update_interval(),
                 [this](Statistics* stat) -> bool {
                   if (!controller_) {
                     return false;
                   }
                   controller_->GetStatistics(stat);
                   return true;
                 }) {
  ::istio::control::tcp::Controller::Options options(config_.tcp_config());

  Utils::CreateEnvironment(cm, dispatcher, random, config_.check_cluster(),
                           config_.report_cluster(), &options.env);

  controller_ = ::istio::control::tcp::Controller::Create(options);

  if (config_.tcp_config().has_report_interval() &&
      config_.tcp_config().report_interval().seconds() >= 0 &&
      config_.tcp_config().report_interval().nanos() >= 0) {
    report_interval_ms_ = std::chrono::milliseconds(
        config_.tcp_config().report_interval().seconds() * 1000 +
        config_.tcp_config().report_interval().nanos() / 1000000);
    // If configured time interval is less than 1 second, then set report
    // interval to 1 second.
    if (report_interval_ms_ < kMinReportIntervalMs) {
      report_interval_ms_ = kMinReportIntervalMs;
    }
  } else {
    report_interval_ms_ = kDefaultReportIntervalMs;
  }
}

}  // namespace Mixer
}  // namespace Tcp
}  // namespace Envoy
