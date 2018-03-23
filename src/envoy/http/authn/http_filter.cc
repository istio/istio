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
#include "common/common/base64.h"
#include "common/http/utility.h"
#include "src/envoy/http/authn/origin_authenticator.h"
#include "src/envoy/http/authn/peer_authenticator.h"
#include "src/envoy/utils/utils.h"

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

// The HTTP header to pass verified authentication payload.
const LowerCaseString AuthenticationFilter::kOutputHeaderLocation(
    "sec-istio-authn-payload");

AuthenticationFilter::AuthenticationFilter(
    const istio::authentication::v1alpha1::Policy& policy)
    : policy_(policy) {}

AuthenticationFilter::~AuthenticationFilter() {}

void AuthenticationFilter::onDestroy() {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
}

FilterHeadersStatus AuthenticationFilter::decodeHeaders(HeaderMap& headers,
                                                        bool) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  state_ = State::PROCESSING;

  filter_context_.reset(new Istio::AuthN::FilterContext(
      &headers, decoder_callbacks_->connection()));

  authenticator_ = createPeerAuthenticator(
      filter_context_.get(),
      [this](bool success) { onPeerAuthenticationDone(success); });
  authenticator_->run();

  if (state_ == State::COMPLETE) {
    return FilterHeadersStatus::Continue;
  }

  stopped_ = true;
  return FilterHeadersStatus::StopIteration;
}

void AuthenticationFilter::onPeerAuthenticationDone(bool success) {
  ENVOY_LOG(debug, "{}: success = {}", __func__, success);
  if (success) {
    authenticator_ = createOriginAuthenticator(
        filter_context_.get(),
        [this](bool success) { onOriginAuthenticationDone(success); });
    authenticator_->run();
  } else {
    rejectRequest("Peer authentication failed.");
  }
}
void AuthenticationFilter::onOriginAuthenticationDone(bool success) {
  ENVOY_LOG(debug, "{}: success = {}", __func__, success);
  if (success) {
    // Put authentication result to headers.
    if (filter_context_ != nullptr) {
      std::string payload_data;
      filter_context_->authenticationResult().SerializeToString(&payload_data);
      const std::string base64_data =
          Base64::encode(payload_data.c_str(), payload_data.size());
      filter_context_->headers()->addReferenceKey(kOutputHeaderLocation,
                                                  base64_data);
    }

    continueDecoding();
  } else {
    rejectRequest("Origin authentication failed.");
  }
}

FilterDataStatus AuthenticationFilter::decodeData(Buffer::Instance&, bool) {
  ENVOY_LOG(debug, "Called AuthenticationFilter : {}", __func__);
  ENVOY_LOG(debug,
            "Called AuthenticationFilter : {} FilterDataStatus::Continue;",
            __FUNCTION__);
  if (state_ == State::PROCESSING) {
    return FilterDataStatus::StopIterationAndBuffer;
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

void AuthenticationFilter::continueDecoding() {
  if (state_ != State::PROCESSING) {
    ENVOY_LOG(error, "State {} is not PROCESSING.", state_);
    return;
  }
  state_ = State::COMPLETE;
  if (stopped_) {
    decoder_callbacks_->continueDecoding();
  }
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
    Istio::AuthN::FilterContext* filter_context,
    const Istio::AuthN::AuthenticatorBase::DoneCallback& done_callback) {
  return std::make_unique<Istio::AuthN::PeerAuthenticator>(
      filter_context, done_callback, policy_);
}

std::unique_ptr<Istio::AuthN::AuthenticatorBase>
AuthenticationFilter::createOriginAuthenticator(
    Istio::AuthN::FilterContext* filter_context,
    const Istio::AuthN::AuthenticatorBase::DoneCallback& done_callback) {
  return std::make_unique<Istio::AuthN::OriginAuthenticator>(
      filter_context, done_callback, policy_);
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
