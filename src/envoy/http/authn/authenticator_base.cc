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

#include "src/envoy/http/authn/authenticator_base.h"
#include "src/envoy/http/authn/authn_utils.h"
#include "src/envoy/utils/utils.h"

using istio::authn::Payload;

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

AuthenticatorBase::AuthenticatorBase(
    FilterContext* filter_context,
    const AuthenticatorBase::DoneCallback& done_callback)
    : filter_context_(*filter_context), done_callback_(done_callback) {}

AuthenticatorBase::~AuthenticatorBase() {}

void AuthenticatorBase::done(bool success) const { done_callback_(success); }

void AuthenticatorBase::validateX509(
    const iaapi::MutualTls& mtls,
    const AuthenticatorBase::MethodDoneCallback& done_callback) const {
  const Network::Connection* connection = filter_context_.connection();
  if (connection == nullptr || connection->ssl() == nullptr) {
    // Not a TLS connection
    done_callback(nullptr, false);
    return;
  }

  Payload payload;
  bool has_user =
      connection->ssl()->peerCertificatePresented() &&
      Utils::GetSourceUser(connection, payload.mutable_x509()->mutable_user());

  if (!has_user && !mtls.allow_tls()) {
    // mTLS and no source user
    done_callback(nullptr, false);
  } else {
    done_callback(&payload, true);
  }
}

void AuthenticatorBase::validateJwt(
    const iaapi::Jwt& jwt,
    const AuthenticatorBase::MethodDoneCallback& done_callback) {
  Payload payload;
  Envoy::Http::HeaderMap& header = *filter_context()->headers();

  auto iter =
      filter_context()->filter_config().jwt_output_payload_locations().find(
          jwt.issuer());
  if (iter ==
      filter_context()->filter_config().jwt_output_payload_locations().end()) {
    ENVOY_LOG(error,
              "No JWT payload header location is found for the issuer {}",
              jwt.issuer());
    done_callback(nullptr, false);
    return;
  }
  LowerCaseString header_key(iter->second);
  bool ret = AuthnUtils::GetJWTPayloadFromHeaders(header, header_key,
                                                  payload.mutable_jwt());
  if (!ret) {
    ENVOY_LOG(debug, "GetJWTPayloadFromHeaders() returns false.");
    done_callback(nullptr, false);
  } else {
    ENVOY_LOG(debug, "A valid JWT is found.");
    // payload is a stack variable, done_callback should treat it only as a
    // temporary variable
    done_callback(&payload, true);
  }
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
