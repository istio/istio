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

#include "service_context.h"

using ::istio::mixer::v1::config::client::ServiceConfig;

namespace istio {
namespace mixer_control {
namespace http {

ServiceContext::ServiceContext(std::shared_ptr<ClientContext> client_context,
                               const ServiceConfig& config)
    : client_context_(client_context), service_config_(config) {
  // Merge client config mixer attributes.
  service_config_.mutable_mixer_attributes()->MergeFrom(
      client_context->config().mixer_attributes());

  // Build quota parser
  for (const auto& quota : service_config_.quota_spec()) {
    quota_parsers_.push_back(
        std::move(::istio::quota::ConfigParser::Create(quota)));
  }
}

// Add static mixer attributes.
void ServiceContext::AddStaticAttributes(RequestContext* request) const {
  if (service_config_.has_mixer_attributes()) {
    request->attributes.MergeFrom(service_config_.mixer_attributes());
  }
}

// Add quota requirements from quota configs.
void ServiceContext::AddQuotas(RequestContext* request) const {
  for (const auto& parser : quota_parsers_) {
    parser->GetRequirements(request->attributes, &request->quotas);
  }
}

}  // namespace http
}  // namespace mixer_control
}  // namespace istio
