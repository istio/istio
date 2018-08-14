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

#include "proxy/src/istio/control/http/controller_impl.h"
#include "proxy/src/istio/control/http/request_handler_impl.h"

using ::istio::mixer::v1::config::client::ServiceConfig;
using ::istio::mixerclient::Statistics;

namespace istio {
namespace control {
namespace http {

namespace {
// The service context cache size.
const int kServiceContextCacheSize = 1000;
}  // namespace

ControllerImpl::ControllerImpl(std::shared_ptr<ClientContext> client_context)
    : client_context_(client_context) {
  int cache_size = client_context_->service_config_cache_size();
  if (cache_size <= 0) {
    cache_size = kServiceContextCacheSize;
  }
  service_context_cache_.reset(new LRUCache(cache_size));
}

ControllerImpl::~ControllerImpl() { service_context_cache_->RemoveAll(); }

bool ControllerImpl::LookupServiceConfig(const std::string& service_config_id) {
  LRUCache::ScopedLookup lookup(service_context_cache_.get(),
                                service_config_id);
  return lookup.Found();
}

void ControllerImpl::AddServiceConfig(
    const std::string& service_config_id,
    const ::istio::mixer::v1::config::client::ServiceConfig& config) {
  CacheElem* cache_elem = new CacheElem;
  cache_elem->service_context =
      std::make_shared<ServiceContext>(client_context_, &config);
  service_context_cache_->Insert(service_config_id, cache_elem, 1);
}

std::unique_ptr<RequestHandler> ControllerImpl::CreateRequestHandler(
    const PerRouteConfig& per_route_config) {
  return std::unique_ptr<RequestHandler>(
      new RequestHandlerImpl(GetServiceContext(per_route_config)));
}

void ControllerImpl::GetStatistics(Statistics* stat) const {
  client_context_->GetStatistics(stat);
}

std::shared_ptr<ServiceContext> ControllerImpl::GetServiceContext(
    const PerRouteConfig& config) {
  if (!config.service_config_id.empty()) {
    LRUCache::ScopedLookup lookup(service_context_cache_.get(),
                                  config.service_config_id);
    if (lookup.Found()) {
      return lookup.value()->service_context;
    }
  }

  const std::string& origin_name = config.destination_service;
  auto service_context = service_context_map_[origin_name];
  if (!service_context) {
    // Get the valid service name from service_configs map.
    auto valid_name = client_context_->GetServiceName(origin_name);
    if (valid_name != origin_name) {
      service_context = service_context_map_[valid_name];
    }
    if (!service_context) {
      service_context = std::make_shared<ServiceContext>(
          client_context_, client_context_->GetServiceConfig(valid_name));
      service_context_map_[valid_name] = service_context;
    }
    if (valid_name != origin_name) {
      service_context_map_[origin_name] = service_context;
    }
  }
  return service_context;
}

std::unique_ptr<Controller> Controller::Create(const Options& data) {
  return std::unique_ptr<Controller>(
      new ControllerImpl(std::make_shared<ClientContext>(data)));
}

}  // namespace http
}  // namespace control
}  // namespace istio
