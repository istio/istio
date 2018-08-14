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

#include "proxy/src/envoy/http/authn/origin_authenticator.h"
#include "authentication/v1alpha1/policy.pb.h"

using istio::authn::Payload;

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

OriginAuthenticator::OriginAuthenticator(FilterContext* filter_context,
                                         const iaapi::Policy& policy)
    : AuthenticatorBase(filter_context), policy_(policy) {}

bool OriginAuthenticator::run(Payload* payload) {
  bool success = false;

  if (policy_.origins_size() == 0) {
    switch (policy_.principal_binding()) {
      case iaapi::PrincipalBinding::USE_ORIGIN:
        // Validation should reject policy that have rule to USE_ORIGIN but
        // does not provide any origin method so this code should
        // never reach. However, it's ok to treat it as authentication
        // fails.
        ENVOY_LOG(warn,
                  "Principal is binded to origin, but no method specified in "
                  "policy {}",
                  policy_.DebugString());
        break;
      case iaapi::PrincipalBinding::USE_PEER:
        // On the other hand, it's ok to have no (origin) methods if
        // rule USE_SOURCE
        success = true;
        break;
      default:
        // Should never come here.
        ENVOY_LOG(error, "Invalid binding value for policy {}",
                  policy_.DebugString());
        break;
    }
  }

  for (const auto& method : policy_.origins()) {
    if (validateJwt(method.jwt(), payload)) {
      success = true;
      break;
    }
  }

  if (success) {
    filter_context()->setOriginResult(payload);
    filter_context()->setPrincipal(policy_.principal_binding());
  }

  return success;
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
