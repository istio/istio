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

#ifndef ISTIO_CONTROL_HTTP_CONTROLLER_IMPL_H
#define ISTIO_CONTROL_HTTP_CONTROLLER_IMPL_H

#include <memory>
#include <unordered_map>

#include "include/istio/control/http/controller.h"
#include "include/istio/utils/simple_lru_cache.h"
#include "include/istio/utils/simple_lru_cache_inl.h"
#include "src/istio/control/http/client_context.h"
#include "src/istio/control/http/service_context.h"

namespace istio {
namespace control {
namespace http {

// The class to implement Controller interface.
class ControllerImpl : public Controller {
 public:
  ControllerImpl(std::shared_ptr<ClientContext> client_context);
  ~ControllerImpl();

  // Lookup a service config by its config id. Return true if found.
  bool LookupServiceConfig(const std::string& service_config_id) override;

  // Add a new service config.
  void AddServiceConfig(
      const std::string& service_config_id,
      const ::istio::mixer::v1::config::client::ServiceConfig& config) override;

  // Creates a HTTP request handler
  std::unique_ptr<RequestHandler> CreateRequestHandler(
      const PerRouteConfig& per_route_config) override;

  // Get statistics.
  void GetStatistics(::istio::mixerclient::Statistics* stat) const override;

 private:
  // Create service config context for HTTP.
  std::shared_ptr<ServiceContext> GetServiceContext(
      const PerRouteConfig& per_route_config);

  // The client context object to hold client config and client cache.
  std::shared_ptr<ClientContext> client_context_;

  // The map to cache service context. key is destination.service
  std::unordered_map<std::string, std::shared_ptr<ServiceContext>>
      service_context_map_;

  // per-route service config may be changed overtime.  A LRU cacahe is used to
  // store used service contexts. ServiceContext initialization is expensive.
  // This cache helps reducing number of ServiceContext creation.
  // The cache has fixed size to control the memory usage. The oldest ones
  // will be purged if the size limit is reached.
  struct CacheElem {
    std::shared_ptr<ServiceContext> service_context;
  };
  using LRUCache = ::istio::utils::SimpleLRUCache<std::string, CacheElem>;
  std::unique_ptr<LRUCache> service_context_cache_;
};

}  // namespace http
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_HTTP_CONTROLLER_IMPL_H
