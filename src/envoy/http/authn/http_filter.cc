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

#include "src/envoy/http/authn/http_filter.h"
#include "src/envoy/http/authn/mtls_authentication.h"
#include "src/envoy/utils/utils.h"

namespace Envoy {
namespace Http {

AuthenticationFilter::AuthenticationFilter(
    const istio::authentication::v1alpha1::Policy& config)
    : config_(config) {}

AuthenticationFilter::~AuthenticationFilter() {}

void AuthenticationFilter::onDestroy() {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
}

FilterHeadersStatus AuthenticationFilter::decodeHeaders(HeaderMap&, bool) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);

  int peer_size = config_.peers_size();
  ENVOY_LOG(debug, "AuthenticationFilter: {} config.peers_size()={}", __func__,
            peer_size);
  if (peer_size > 0) {
    const auto& m = config_.peers()[0];
    if (m.has_mtls()) {
      ENVOY_LOG(debug, "AuthenticationFilter: {} this connection requires mTLS",
                __func__);
      MtlsAuthentication mtls_authn(decoder_callbacks_->connection());
      if (mtls_authn.IsMutualTLS() == false) {
        // In prototype, only log the authentication policy violation.
        ENVOY_LOG(error,
                  "AuthenticationFilter: authn policy requires mTLS but the "
                  "connection is not mTLS!");
      } else {
        ENVOY_LOG(debug, "AuthenticationFilter: the connection is mTLS.");
        std::string user, ip;
        int port = 0;
        bool ret = false;
        ret = mtls_authn.GetSourceUser(&user);
        if (ret) {
          ENVOY_LOG(debug, "AuthenticationFilter: the source user is {}", user);
        } else {
          ENVOY_LOG(error,
                    "AuthenticationFilter: GetSourceUser() returns false!");
        }
        ret = mtls_authn.GetSourceIpPort(&ip, &port);
        if (ret) {
          ENVOY_LOG(debug,
                    "AuthenticationFilter: the source ip is {}, the source "
                    "port is {}",
                    user, port);
        } else {
          ENVOY_LOG(error,
                    "AuthenticationFilter: GetSourceIpPort() returns false!");
        }
      }
    } else {
      ENVOY_LOG(
          debug,
          "AuthenticationFilter: {} this connection does not require mTLS",
          __func__);
    }
  }

  ENVOY_LOG(
      debug,
      "Called AuthenticationFilter : {}, return FilterHeadersStatus::Continue;",
      __func__);
  return FilterHeadersStatus::Continue;
}

FilterDataStatus AuthenticationFilter::decodeData(Buffer::Instance&, bool) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  ENVOY_LOG(debug,
            "Called AuthenticationFilter : {} FilterDataStatus::Continue;",
            __FUNCTION__);
  return FilterDataStatus::Continue;
}

FilterTrailersStatus AuthenticationFilter::decodeTrailers(HeaderMap&) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  return FilterTrailersStatus::Continue;
}

void AuthenticationFilter::setDecoderFilterCallbacks(
    StreamDecoderFilterCallbacks& callbacks) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  decoder_callbacks_ = &callbacks;
}

}  // namespace Http
}  // namespace Envoy
