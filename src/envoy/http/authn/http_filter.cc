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
#include "authentication/v1alpha1/policy.pb.h"
#include "common/http/utility.h"
#include "envoy/config/filter/http/authn/v2alpha1/config.pb.h"
#include "src/envoy/http/authn/origin_authenticator.h"
#include "src/envoy/http/authn/peer_authenticator.h"
#include "src/envoy/utils/authn.h"
#include "src/envoy/utils/utils.h"

using istio::authn::Payload;
using istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig;

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

AuthenticationFilter::AuthenticationFilter(const FilterConfig& filter_config)
    : filter_config_(filter_config) {}

AuthenticationFilter::~AuthenticationFilter() {}

void AuthenticationFilter::onDestroy() {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
}

FilterHeadersStatus AuthenticationFilter::decodeHeaders(HeaderMap& headers,
                                                        bool) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  state_ = State::PROCESSING;

  filter_context_.reset(new Istio::AuthN::FilterContext(
      &headers, decoder_callbacks_->connection(), filter_config_));

  Payload payload;

  if (!createPeerAuthenticator(filter_context_.get())->run(&payload)) {
    rejectRequest("Peer authentication failed.");
    return FilterHeadersStatus::StopIteration;
  }

  bool success =
      createOriginAuthenticator(filter_context_.get())->run(&payload);

  // After Istio authn, the JWT headers consumed by Istio authn should be
  // removed.
  // TODO: remove internal headers used to pass data between filters
  // https://github.com/istio/istio/issues/4689
  for (auto const iter : filter_config_.jwt_output_payload_locations()) {
    filter_context_->headers()->remove(LowerCaseString(iter.second));
  }

  if (!success) {
    rejectRequest("Origin authentication failed.");
    return FilterHeadersStatus::StopIteration;
  }

  // Put authentication result to headers.
  if (filter_context_ != nullptr) {
    Utils::Authentication::SaveResultToHeader(
        filter_context_->authenticationResult(), filter_context_->headers());
  }
  state_ = State::COMPLETE;
  return FilterHeadersStatus::Continue;
}

FilterDataStatus AuthenticationFilter::decodeData(Buffer::Instance&, bool) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  ENVOY_LOG(debug,
            "Called AuthenticationFilter : {} FilterDataStatus::Continue;",
            __FUNCTION__);
  if (state_ == State::PROCESSING) {
    return FilterDataStatus::StopIterationAndWatermark;
  }
  return FilterDataStatus::Continue;
}

FilterTrailersStatus AuthenticationFilter::decodeTrailers(HeaderMap&) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  if (state_ == State::PROCESSING) {
    return FilterTrailersStatus::StopIteration;
  }
  return FilterTrailersStatus::Continue;
}

void AuthenticationFilter::setDecoderFilterCallbacks(
    StreamDecoderFilterCallbacks& callbacks) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  decoder_callbacks_ = &callbacks;
}

void AuthenticationFilter::rejectRequest(const std::string& message) {
  if (state_ != State::PROCESSING) {
    ENVOY_LOG(error, "State {} is not PROCESSING.", state_);
    return;
  }
  state_ = State::REJECTED;
  Utility::sendLocalReply(*decoder_callbacks_, false, Http::Code::Unauthorized,
                          message);
}

std::unique_ptr<Istio::AuthN::AuthenticatorBase>
AuthenticationFilter::createPeerAuthenticator(
    Istio::AuthN::FilterContext* filter_context) {
  return std::make_unique<Istio::AuthN::PeerAuthenticator>(
      filter_context, filter_config_.policy());
}

std::unique_ptr<Istio::AuthN::AuthenticatorBase>
AuthenticationFilter::createOriginAuthenticator(
    Istio::AuthN::FilterContext* filter_context) {
  return std::make_unique<Istio::AuthN::OriginAuthenticator>(
      filter_context, filter_config_.policy());
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
