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

#include "src/envoy/mixer/mixer_control.h"

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// Default time interval for periodical report is 10 seconds.
const std::chrono::milliseconds kDefaultReportIntervalMs(10000);

// Minimum time interval for periodical report is 1 seconds.
const std::chrono::milliseconds kMinReportIntervalMs(1000);

// A class to wrap envoy timer for mixer client timer.
class EnvoyTimer : public ::istio::mixer_client::Timer {
 public:
  EnvoyTimer(Event::TimerPtr timer) : timer_(std::move(timer)) {}

  void Stop() override { timer_->disableTimer(); }
  void Start(int interval_ms) override {
    timer_->enableTimer(std::chrono::milliseconds(interval_ms));
  }

 private:
  Event::TimerPtr timer_;
};

// Create all environment functions.
void CreateEnvironment(Upstream::ClusterManager& cm,
                       Event::Dispatcher& dispatcher,
                       Runtime::RandomGenerator& random,
                       const std::string& check_cluster,
                       const std::string& report_cluster,
                       ::istio::mixer_client::Environment* env) {
  env->check_transport = CheckTransport::GetFunc(cm, check_cluster, nullptr);
  env->report_transport = ReportTransport::GetFunc(cm, report_cluster);

  env->timer_create_func = [&dispatcher](std::function<void()> timer_cb)
      -> std::unique_ptr<::istio::mixer_client::Timer> {
        return std::unique_ptr<::istio::mixer_client::Timer>(
            new EnvoyTimer(dispatcher.createTimer(timer_cb)));
      };

  env->uuid_generate_func = [&random]() -> std::string {
    return random.uuid();
  };
}

}  // namespace

HttpMixerControl::HttpMixerControl(const HttpMixerConfig& mixer_config,
                                   Upstream::ClusterManager& cm,
                                   Event::Dispatcher& dispatcher,
                                   Runtime::RandomGenerator& random)
    : config_(mixer_config), cm_(cm) {
  ::istio::mixer_control::http::Controller::Options options(
      mixer_config.http_config, mixer_config.legacy_quotas);

  CreateEnvironment(cm, dispatcher, random, config_.check_cluster(),
                    config_.report_cluster(), &options.env);

  controller_ = ::istio::mixer_control::http::Controller::Create(options);
  has_v2_config_ = mixer_config.has_v2_config;
}

TcpMixerControl::TcpMixerControl(const TcpMixerConfig& mixer_config,
                                 Upstream::ClusterManager& cm,
                                 Event::Dispatcher& dispatcher,
                                 Runtime::RandomGenerator& random)
    : config_(mixer_config), dispatcher_(dispatcher) {
  ::istio::mixer_control::tcp::Controller::Options options(
      mixer_config.tcp_config);

  CreateEnvironment(cm, dispatcher, random, config_.check_cluster(),
                    config_.report_cluster(), &options.env);

  controller_ = ::istio::mixer_control::tcp::Controller::Create(options);

  if (mixer_config.tcp_config.has_report_interval() &&
      mixer_config.tcp_config.report_interval().seconds() >= 0 &&
      mixer_config.tcp_config.report_interval().nanos() >= 0) {
    report_interval_ms_ = std::chrono::milliseconds(
        mixer_config.tcp_config.report_interval().seconds() * 1000 +
        mixer_config.tcp_config.report_interval().nanos() / 1000000);
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
}  // namespace Http
}  // namespace Envoy
