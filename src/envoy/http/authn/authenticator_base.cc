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

AuthenticatorBase::AuthenticatorBase(FilterContext* filter_context)
    : filter_context_(*filter_context) {}

AuthenticatorBase::~AuthenticatorBase() {}

bool AuthenticatorBase::validateX509(const iaapi::MutualTls& mtls,
                                     Payload* payload) const {
  const Network::Connection* connection = filter_context_.connection();
  if (connection == nullptr || connection->ssl() == nullptr) {
    // Not a TLS connection
    return false;
  }

  bool has_user =
      connection->ssl()->peerCertificatePresented() &&
      Utils::GetSourceUser(connection, payload->mutable_x509()->mutable_user());

  return has_user || mtls.allow_tls();
}

bool AuthenticatorBase::validateJwt(const iaapi::Jwt& jwt, Payload* payload) {
  Envoy::Http::HeaderMap& header = *filter_context()->headers();

  auto iter =
      filter_context()->filter_config().jwt_output_payload_locations().find(
          jwt.issuer());
  if (iter ==
      filter_context()->filter_config().jwt_output_payload_locations().end()) {
    ENVOY_LOG(warn, "No JWT payload header location is found for the issuer {}",
              jwt.issuer());
    return false;
  }

  LowerCaseString header_key(iter->second);
  return AuthnUtils::GetJWTPayloadFromHeaders(header, header_key,
                                              payload->mutable_jwt());
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
