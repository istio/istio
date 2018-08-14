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

#include "proxy/src/istio/control/http/client_context.h"

using ::istio::mixer::v1::config::client::ServiceConfig;

namespace istio {
namespace control {
namespace http {

ClientContext::ClientContext(const Controller::Options& data)
    : ClientContextBase(data.config.transport(), data.env),
      config_(data.config),
      service_config_cache_size_(data.service_config_cache_size) {}

ClientContext::ClientContext(
    std::unique_ptr<::istio::mixerclient::MixerClient> mixer_client,
    const ::istio::mixer::v1::config::client::HttpClientConfig& config,
    int service_config_cache_size)
    : ClientContextBase(std::move(mixer_client)),
      config_(config),
      service_config_cache_size_(service_config_cache_size) {}

const std::string& ClientContext::GetServiceName(
    const std::string& service_name) const {
  if (service_name.empty()) {
    return config_.default_destination_service();
  }
  const auto& config_map = config_.service_configs();
  auto it = config_map.find(service_name);
  if (it == config_map.end()) {
    return config_.default_destination_service();
  }
  return service_name;
}

// Get the service config by the name.
const ServiceConfig* ClientContext::GetServiceConfig(
    const std::string& service_name) const {
  const auto& config_map = config_.service_configs();
  auto it = config_map.find(service_name);
  if (it != config_map.end()) {
    return &it->second;
  }
  return nullptr;
}

}  // namespace http
}  // namespace control
}  // namespace istio
