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

#include "src/envoy/http/authn/peer_authenticator.h"
#include "common/http/utility.h"
#include "src/envoy/utils/utils.h"

using istio::authn::Payload;

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

PeerAuthenticator::PeerAuthenticator(FilterContext* filter_context,
                                     const iaapi::Policy& policy)
    : AuthenticatorBase(filter_context), policy_(policy) {}

bool PeerAuthenticator::run(Payload* payload) {
  bool success = false;
  if (policy_.peers_size() == 0) {
    ENVOY_LOG(debug, "No method defined. Skip source authentication.");
    success = true;
    return success;
  }
  for (const auto& method : policy_.peers()) {
    switch (method.params_case()) {
      case iaapi::PeerAuthenticationMethod::ParamsCase::kMtls:
        success = validateX509(method.mtls(), payload);
        break;
      case iaapi::PeerAuthenticationMethod::ParamsCase::kJwt:
        success = validateJwt(method.jwt(), payload);
        break;
      default:
        ENVOY_LOG(error, "Unknown peer authentication param {}",
                  method.DebugString());
        success = false;
        break;
    }

    if (success) {
      break;
    }
  }

  if (success) {
    filter_context()->setPeerResult(payload);
  }

  return success;
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
