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

#include "src/envoy/http/mixer/config.h"
#include "src/envoy/utils/config.h"

using ::istio::mixer::v1::config::client::EndUserAuthenticationPolicySpec;

namespace Envoy {
namespace Http {
namespace Mixer {

Config::Config(
    const ::istio::mixer::v1::config::client::HttpClientConfig& config_pb)
    : config_pb_(config_pb) {
  Utils::SetDefaultMixerClusters(config_pb_.mutable_transport());
}

// Extract all AuthSpec from all service config.
std::unique_ptr<EndUserAuthenticationPolicySpec> Config::auth_config() {
  auto spec = std::unique_ptr<EndUserAuthenticationPolicySpec>(
      new EndUserAuthenticationPolicySpec);
  for (const auto& it : config_pb_.service_configs()) {
    if (it.second.has_end_user_authn_spec()) {
      spec->MergeFrom(it.second.end_user_authn_spec());
    }
  }
  if (spec->jwts_size() == 0) {
    spec.reset();
  }
  return spec;
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
