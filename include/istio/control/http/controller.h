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

#ifndef ISTIO_CONTROL_HTTP_CONTROLLER_H
#define ISTIO_CONTROL_HTTP_CONTROLLER_H

#include "include/istio/control/http/request_handler.h"
#include "include/istio/mixerclient/client.h"
#include "mixer/v1/config/client/client_config.pb.h"

namespace istio {
namespace control {
namespace http {

// An interface to support Mixer control.
// It takes MixerFitlerConfig and performs tasks to enforce
// mixer control over HTTP and TCP requests.
class Controller {
 public:
  virtual ~Controller() {}

  // Following two functions are used to manage service configs.
  // Callers should call LookupServiceConfig to lookup the config with id
  // and use AddServiceConfig to add the config if it is new.

  // Lookup a service config by its config id. Return true if found.
  virtual bool LookupServiceConfig(const std::string& service_config_id) = 0;

  // Add a new service config.
  virtual void AddServiceConfig(
      const std::string& service_config_id,
      const ::istio::mixer::v1::config::client::ServiceConfig& config) = 0;

  // A data struct to pass in per-route config.
  struct PerRouteConfig {
    // The per route destination.server name.
    // It will be used to lookup per route config map.
    std::string destination_service;

    // A unique ID to identify a config version for a service.
    // Usually it is a sha of the whole service config.
    // The config should have been added by AddServiceConfig().
    // If it is empty, destination_service is used to lookup
    // service_configs map in the HttpClientConfig.
    std::string service_config_id;
  };

  // Creates a HTTP request handler.
  // The handler supports making Check and Report calls to Mixer.
  // "per_route_config" is for supporting older version of Pilot which
  // set per-route config in route opaque data.
  virtual std::unique_ptr<RequestHandler> CreateRequestHandler(
      const PerRouteConfig& per_route_config) = 0;

  // The initial data required by the Controller. It needs:
  // * client_config: the mixer client config.
  // * some functions provided by the environment (Envoy)
  // * optional service config cache size.
  struct Options {
    Options(const ::istio::mixer::v1::config::client::HttpClientConfig& config)
        : config(config) {}

    // Mixer filter config
    const ::istio::mixer::v1::config::client::HttpClientConfig& config;

    // Some plaform functions for mixer client library.
    ::istio::mixerclient::Environment env;

    // The LRU cache size for service config.
    // If not set or is 0 default value, the cache size is 1000.
    int service_config_cache_size{};
  };

  // The factory function to create a new instance of the controller.
  static std::unique_ptr<Controller> Create(const Options& options);

  // Get statistics.
  virtual void GetStatistics(::istio::mixerclient::Statistics* stat) const = 0;
};

}  // namespace http
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_HTTP_CONTROLLER_H
