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

PeerAuthenticator::PeerAuthenticator(
    FilterContext* filter_context,
    const AuthenticatorBase::DoneCallback& done_callback,
    const iaapi::Policy& policy)
    : AuthenticatorBase(filter_context, done_callback), policy_(policy) {}

void PeerAuthenticator::run() {
  if (policy_.peers_size() == 0) {
    ENVOY_LOG(debug, "No method defined. Skip source authentication.");
    onMethodDone(nullptr, true);
  } else {
    runMethod(policy_.peers(0), [this](const Payload* payload, bool success) {
      onMethodDone(payload, success);
    });
  }
}

void PeerAuthenticator::runMethod(const iaapi::PeerAuthenticationMethod& method,
                                  const MethodDoneCallback& done_callback) {
  switch (method.params_case()) {
    case iaapi::PeerAuthenticationMethod::ParamsCase::kMtls:
      validateX509(method.mtls(), done_callback);
      break;
    case iaapi::PeerAuthenticationMethod::ParamsCase::kJwt:
      validateJwt(method.jwt(), done_callback);
      break;
    case iaapi::PeerAuthenticationMethod::ParamsCase::kNone:
      done_callback(nullptr, true);
      break;
    default:
      ENVOY_LOG(error, "Unknown peer authentication param {}",
                method.DebugString());
      done_callback(nullptr, false);
  }
}
void PeerAuthenticator::onMethodDone(const Payload* payload, bool success) {
  if (!success && peer_method_index_ + 1 < policy_.peers_size()) {
    // Authentication fails, try next one if available.
    peer_method_index_++;
    runMethod(policy_.peers(peer_method_index_),
              [this](const Payload* payload, bool success) {
                onMethodDone(payload, success);
              });
    return;
  }

  if (success) {
    filter_context()->setPeerResult(payload);
  }
  done(success);
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
