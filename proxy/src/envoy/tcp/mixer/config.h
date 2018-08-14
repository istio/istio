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

#include "mixer/v1/config/client/client_config.pb.h"
#include "src/envoy/utils/config.h"

namespace Envoy {
namespace Tcp {
namespace Mixer {
namespace {

// Default time interval for periodical report is 10 seconds.
const std::chrono::milliseconds kDefaultReportIntervalMs(10000);

// Minimum time interval for periodical report is 1 seconds.
const std::chrono::milliseconds kMinReportIntervalMs(1000);

}  // namespace

// Config for tcp filter.
class Config {
 public:
  Config(const ::istio::mixer::v1::config::client::TcpClientConfig& config_pb)
      : config_pb_(config_pb) {
    Utils::SetDefaultMixerClusters(config_pb_.mutable_transport());

    if (config_pb_.has_report_interval() &&
        config_pb_.report_interval().seconds() >= 0 &&
        config_pb_.report_interval().nanos() >= 0) {
      report_interval_ms_ = std::chrono::milliseconds(
          config_pb_.report_interval().seconds() * 1000 +
          config_pb_.report_interval().nanos() / 1000000);
      // If configured time interval is less than 1 second, then set report
      // interval to 1 second.
      if (report_interval_ms_ < kMinReportIntervalMs) {
        report_interval_ms_ = kMinReportIntervalMs;
      }
    } else {
      report_interval_ms_ = kDefaultReportIntervalMs;
    }
  }

  // The Tcp client config.
  const ::istio::mixer::v1::config::client::TcpClientConfig& config_pb() const {
    return config_pb_;
  }

  // check cluster
  const std::string& check_cluster() const {
    return config_pb_.transport().check_cluster();
  }
  // report cluster
  const std::string& report_cluster() const {
    return config_pb_.transport().report_cluster();
  }

  std::chrono::milliseconds report_interval_ms() const {
    return report_interval_ms_;
  }

 private:
  // The Tcp client config.
  ::istio::mixer::v1::config::client::TcpClientConfig config_pb_;
  // Time interval in milliseconds for sending periodical delta reports.
  std::chrono::milliseconds report_interval_ms_;
};

}  // namespace Mixer
}  // namespace Tcp
}  // namespace Envoy
