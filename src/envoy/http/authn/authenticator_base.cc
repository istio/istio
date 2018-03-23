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
#include "src/envoy/http/authn/mtls_authentication.h"

using istio::authn::Payload;

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {
namespace {
// The HTTP header from which to get the verified Jwt result.
// It is currently hard-coded. After jwt-auth has a
// parameter for this header, the hardcoded parameter will
// be removed.
const LowerCaseString kJwtHeaderKey("sec-istio-auth-userinfo");

}  // namespace

AuthenticatorBase::AuthenticatorBase(
    FilterContext* filter_context,
    const AuthenticatorBase::DoneCallback& done_callback)
    : filter_context_(*filter_context), done_callback_(done_callback) {}

AuthenticatorBase::~AuthenticatorBase() {}

void AuthenticatorBase::done(bool success) const { done_callback_(success); }

void AuthenticatorBase::validateX509(
    const iaapi::MutualTls&,
    const AuthenticatorBase::MethodDoneCallback& done_callback) const {
  // Boilerplate for x509 validation and extraction. This function should
  // extract user from SAN field from the x509 certificate come with request.
  // (validation might not be needed, as establisment of the connection by
  // itself is validation).
  // If x509 is missing (i.e connection is not on TLS) or SAN value is not
  // legit, call callback with status FAILED.
  ENVOY_LOG(debug, "AuthenticatorBase: {} this connection requires mTLS",
            __func__);
  MtlsAuthentication mtls_authn(filter_context_.connection());
  if (mtls_authn.IsMutualTLS() == false) {
    done_callback(nullptr, false);
    return;
  }

  Payload payload;
  if (!mtls_authn.GetSourceUser(payload.mutable_x509()->mutable_user())) {
    done_callback(&payload, false);
  }

  // TODO (lei-tang): Adding other attributes (i.e ip) to payload if needed.
  done_callback(&payload, true);
}

void AuthenticatorBase::validateJwt(
    const iaapi::Jwt&,
    const AuthenticatorBase::MethodDoneCallback& done_callback) {
  Payload payload;
  Envoy::Http::HeaderMap& header = *filter_context()->headers();
  ENVOY_LOG(debug, "{} the number of headers is {}", __func__, header.size());

  bool ret = AuthnUtils::GetJWTPayloadFromHeaders(header, kJwtHeaderKey,
                                                  payload.mutable_jwt());
  if (!ret) {
    ENVOY_LOG(debug,
              "AuthenticatorBase: {} GetJWTPayloadFromHeaders() returns false.",
              __func__);
    done_callback(nullptr, false);
  } else {
    ENVOY_LOG(debug, "AuthenticatorBase: {}(): a valid JWT is found.",
              __func__);
    // payload is a stack variable, done_callback should treat it only as a
    // temporary variable
    done_callback(&payload, true);
  }
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
