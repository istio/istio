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
#include "src/envoy/utils/filter_names.h"
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

FilterHeadersStatus AuthenticationFilter::decodeHeaders(HeaderMap&, bool) {
  ENVOY_LOG(debug, "AuthenticationFilter::decodeHeaders with config\n{}",
            filter_config_.DebugString());
  state_ = State::PROCESSING;

  filter_context_.reset(new Istio::AuthN::FilterContext(
      decoder_callbacks_->requestInfo().dynamicMetadata(),
      decoder_callbacks_->connection(), filter_config_));

  Payload payload;

  if (!filter_config_.policy().peer_is_optional() &&
      !createPeerAuthenticator(filter_context_.get())->run(&payload)) {
    rejectRequest("Peer authentication failed.");
    return FilterHeadersStatus::StopIteration;
  }

  bool success =
      filter_config_.policy().origin_is_optional() ||
      createOriginAuthenticator(filter_context_.get())->run(&payload);

  if (!success) {
    rejectRequest("Origin authentication failed.");
    return FilterHeadersStatus::StopIteration;
  }

  // Put authentication result to headers.
  if (filter_context_ != nullptr) {
    // Save auth results in the metadata, could be used later by RBAC and/or
    // mixer filter.
    ProtobufWkt::Struct data;
    Utils::Authentication::SaveAuthAttributesToStruct(
        filter_context_->authenticationResult(), data);
    decoder_callbacks_->requestInfo().setDynamicMetadata(
        Utils::IstioFilterName::kAuthentication, data);
    ENVOY_LOG(debug, "Saved Dynamic Metadata:\n{}", data.DebugString());
  }
  state_ = State::COMPLETE;
  return FilterHeadersStatus::Continue;
}

FilterDataStatus AuthenticationFilter::decodeData(Buffer::Instance&, bool) {
  if (state_ == State::PROCESSING) {
    return FilterDataStatus::StopIterationAndWatermark;
  }
  return FilterDataStatus::Continue;
}

FilterTrailersStatus AuthenticationFilter::decodeTrailers(HeaderMap&) {
  if (state_ == State::PROCESSING) {
    return FilterTrailersStatus::StopIteration;
  }
  return FilterTrailersStatus::Continue;
}

void AuthenticationFilter::setDecoderFilterCallbacks(
    StreamDecoderFilterCallbacks& callbacks) {
  decoder_callbacks_ = &callbacks;
}

void AuthenticationFilter::rejectRequest(const std::string& message) {
  if (state_ != State::PROCESSING) {
    return;
  }
  state_ = State::REJECTED;
  decoder_callbacks_->sendLocalReply(Http::Code::Unauthorized, message,
                                     nullptr);
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
