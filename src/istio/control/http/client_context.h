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

#ifndef ISTIO_CONTROL_HTTP_CLIENT_CONTEXT_H
#define ISTIO_CONTROL_HTTP_CLIENT_CONTEXT_H

#include "include/istio/control/http/controller.h"
#include "src/istio/control/client_context_base.h"

namespace istio {
namespace control {
namespace http {

// The global context object to hold:
// * the mixer client config
// * the mixer client object to call Check/Report with cache.
class ClientContext : public ClientContextBase {
 public:
  ClientContext(const Controller::Options& data);
  // A constructor for unit-test to pass in a mock mixer_client
  ClientContext(
      std::unique_ptr<::istio::mixerclient::MixerClient> mixer_client,
      const ::istio::mixer::v1::config::client::HttpClientConfig& config,
      int service_config_cache_size);

  // Retrieve mixer client config.
  const ::istio::mixer::v1::config::client::HttpClientConfig& config() const {
    return config_;
  }

  // Get valid service name in the config map.
  // If input service name is in the map, use it, otherwise, use the default
  // one.
  const std::string& GetServiceName(const std::string& service_name) const;

  // Get the service config by the name.
  const ::istio::mixer::v1::config::client::ServiceConfig* GetServiceConfig(
      const std::string& service_name) const;

  // Get the service config cache size
  int service_config_cache_size() const { return service_config_cache_size_; }

 private:
  // The http client config.
  const ::istio::mixer::v1::config::client::HttpClientConfig& config_;

  // The service config cache size
  int service_config_cache_size_;
};

}  // namespace http
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_HTTP_CLIENT_CONTEXT_H
