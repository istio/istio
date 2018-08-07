/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#include "src/envoy/http/authn/filter_context.h"
#include "src/envoy/utils/filter_names.h"
#include "src/envoy/utils/utils.h"

using istio::authn::Payload;
using istio::authn::Result;

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

void FilterContext::setPeerResult(const Payload* payload) {
  if (payload != nullptr) {
    switch (payload->payload_case()) {
      case Payload::kX509:
        ENVOY_LOG(debug, "Set peer from X509: {}", payload->x509().user());
        result_.set_peer_user(payload->x509().user());
        break;
      case Payload::kJwt:
        ENVOY_LOG(debug, "Set peer from JWT: {}", payload->jwt().user());
        result_.set_peer_user(payload->jwt().user());
        break;
      default:
        ENVOY_LOG(debug, "Payload has not peer authentication data");
        break;
    }
  }
}
void FilterContext::setOriginResult(const Payload* payload) {
  // Authentication pass, look at the return payload and store to the context
  // output. Set filter to continueDecoding when done.
  // At the moment, only JWT can be used for origin authentication, so
  // it's ok just to check jwt payload.
  if (payload != nullptr && payload->has_jwt()) {
    *result_.mutable_origin() = payload->jwt();
  }
}

void FilterContext::setPrincipal(const iaapi::PrincipalBinding& binding) {
  switch (binding) {
    case iaapi::PrincipalBinding::USE_PEER:
      ENVOY_LOG(debug, "Set principal from peer: {}", result_.peer_user());
      result_.set_principal(result_.peer_user());
      return;
    case iaapi::PrincipalBinding::USE_ORIGIN:
      ENVOY_LOG(debug, "Set principal from origin: {}",
                result_.origin().user());
      result_.set_principal(result_.origin().user());
      return;
    default:
      // Should never come here.
      ENVOY_LOG(error, "Invalid binding value {}", binding);
      return;
  }
}

bool FilterContext::getJwtPayload(const std::string& issuer,
                                  std::string* payload) const {
  const auto filter_it =
      dynamic_metadata_.filter_metadata().find(Utils::IstioFilterName::kJwt);
  if (filter_it == dynamic_metadata_.filter_metadata().end()) {
    ENVOY_LOG(debug, "No dynamic_metadata found for filter {}",
              Utils::IstioFilterName::kJwt);
    return false;
  }

  const auto& data_struct = filter_it->second;
  const auto entry_it = data_struct.fields().find(issuer);
  if (entry_it == data_struct.fields().end()) {
    return false;
  }
  if (entry_it->second.string_value().empty()) {
    return false;
  }

  *payload = entry_it->second.string_value();
  return true;
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
