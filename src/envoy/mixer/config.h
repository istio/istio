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

  // check cluster
  const std::string& check_cluster() const {
    return http_config.transport().check_cluster();
  }
  // report cluster
  const std::string& report_cluster() const {
    return http_config.transport().report_cluster();
  }
};

// Config for tcp filter.
struct TcpMixerConfig {
  // Load from envoy filter config in JSON format.
  void Load(const Json::Object& json);

  // The Tcp client config.
  ::istio::mixer::v1::config::client::TcpClientConfig tcp_config;

  // check cluster
  const std::string& check_cluster() const {
    return tcp_config.transport().check_cluster();
  }
  // report cluster
  const std::string& report_cluster() const {
    return tcp_config.transport().report_cluster();
  }
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
