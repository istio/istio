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

#include "controller_impl.h"
#include "request_handler_impl.h"

using ::istio::mixer::v1::config::client::ServiceConfig;
using ::istio::mixer_client::Statistics;

namespace istio {
namespace mixer_control {
namespace http {

ControllerImpl::ControllerImpl(const Options& data) {
  client_context_.reset(new ClientContext(data));
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
  // If use legacy config
  if (config.legacy_config) {
    return std::make_shared<ServiceContext>(client_context_,
                                            config.legacy_config);
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
  return std::unique_ptr<Controller>(new ControllerImpl(data));
}

}  // namespace http
}  // namespace mixer_control
}  // namespace istio
