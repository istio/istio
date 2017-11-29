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

#include <map>
#include <string>
#include <vector>

#include "common/common/logger.h"
#include "envoy/json/json_object.h"
#include "mixer/v1/config/client/client_config.pb.h"
#include "quota/include/requirement.h"

namespace Envoy {
namespace Http {
namespace Mixer {

// Config for http filter.
struct HttpMixerConfig {
  // Load from envoy filter config in JSON format.
  void Load(const Json::Object& json);

  // The Http client config.
  ::istio::mixer::v1::config::client::HttpClientConfig http_config;
  // legacy quota requirment
  std::vector<::istio::quota::Requirement> legacy_quotas;
  // If true, v2 config is valid.
  bool has_v2_config;

  // Create per route legacy config.
  static void CreateLegacyRouteConfig(
      bool disable_check, bool disable_report,
      const std::map<std::string, std::string>& attributes,
      ::istio::mixer::v1::config::client::ServiceConfig* config);
};

// Config for tcp filter.
struct TcpMixerConfig {
  // Load from envoy filter config in JSON format.
  void Load(const Json::Object& json);

  // The Tcp client config.
  ::istio::mixer::v1::config::client::TcpClientConfig tcp_config;
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
