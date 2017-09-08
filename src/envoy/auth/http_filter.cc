/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#include "http_filter.h"
#include "config.h"
#include "jwt.h"

#include "common/http/message_impl.h"
#include "common/http/utility.h"
#include "envoy/http/async_client.h"
#include "server/config/network/http_connection_manager.h"

#include <chrono>
#include <string>

namespace Envoy {
namespace Http {

const LowerCaseString& JwtVerificationFilter::AuthorizedHeaderKey() {
  static LowerCaseString* key = new LowerCaseString("sec-istio-auth-userinfo");
  return *key;
}

JwtVerificationFilter::JwtVerificationFilter(
    std::shared_ptr<Auth::JwtAuthConfig> config)
    : config_(config) {}

JwtVerificationFilter::~JwtVerificationFilter() {}

void JwtVerificationFilter::onDestroy() {
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {}", __func__);
  state_ = Responded;
  // Cancelling all request for public keys
  for (const auto& iss_kv : calling_issuers_) {
    iss_kv.second.second->Cancel();
  }
}

FilterHeadersStatus JwtVerificationFilter::decodeHeaders(HeaderMap& headers,
                                                         bool) {
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {}", __func__);
  state_ = Calling;
  stopped_ = false;

  /*
   * TODO: update cached public key regularly
   * TODO: handle the case that the next request comes before fetching public
   * key completed
   */

  // list up issuers whose public key should be fetched
  for (const auto& iss : config_->issuers_) {
    if (!iss->failed_ && !iss->loaded_) {
      calling_issuers_[iss->name_] = std::make_pair(iss, nullptr);
    }
  }
  // send HTTP requests to fetch public keys
  if (!calling_issuers_.empty()) {
    for (const auto& iss : config_->issuers_) {
      if (iss->failed_ || iss->loaded_) {
        continue;
      }
      calling_issuers_[iss->name_].second =
          std::unique_ptr<Auth::AsyncClientCallbacks>(
              new Auth::AsyncClientCallbacks(
                  config_->cm_, iss->cluster_,
                  [&](bool succeed, const std::string& pubkey) -> void {
                    this->ReceivePubkey(headers, iss->name_, succeed, pubkey);
                  }));
      calling_issuers_[iss->name_].second->Call(iss->uri_);
    }
  } else {
    // If we do not need to fetch any public keys, just proceed to verification.
    CompleteVerification(headers);
  }

  if (state_ == Complete) {
    return FilterHeadersStatus::Continue;
  }
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {} Stop", __func__);
  stopped_ = true;
  return FilterHeadersStatus::StopIteration;
}

FilterDataStatus JwtVerificationFilter::decodeData(Buffer::Instance&, bool) {
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {}", __func__);
  if (state_ == Calling) {
    return FilterDataStatus::StopIterationAndBuffer;
  }
  return FilterDataStatus::Continue;
}

FilterTrailersStatus JwtVerificationFilter::decodeTrailers(HeaderMap&) {
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {}", __func__);
  if (state_ == Calling) {
    return FilterTrailersStatus::StopIteration;
  }
  return FilterTrailersStatus::Continue;
}

void JwtVerificationFilter::setDecoderFilterCallbacks(
    StreamDecoderFilterCallbacks& callbacks) {
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {}", __func__);
  decoder_callbacks_ = &callbacks;
}

void JwtVerificationFilter::ReceivePubkey(HeaderMap& headers,
                                          std::string issuer_name, bool succeed,
                                          const std::string& pubkey) {
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {} , issuer = {}", __func__,
            issuer_name);
  auto iss_it = calling_issuers_.find(issuer_name);
  auto& iss = iss_it->second.first;
  iss->failed_ = !succeed;
  if (succeed) {
    iss->pkey_ = Auth::Pubkeys::CreateFrom(pubkey, iss->pkey_type_);
  }
  iss->loaded_ = true;
  calling_issuers_.erase(iss_it);

  // if receive all responses, proceed to verification
  if (calling_issuers_.empty()) {
    CompleteVerification(headers);
  }
}

/*
 * TODO: status as enum class
 */
std::string JwtVerificationFilter::Verify(HeaderMap& headers) {
  const HeaderEntry* entry = headers.get(kAuthorizationHeaderKey);
  if (!entry) {
    return "NO_AUTHORIZATION_HEADER";
  }
  const HeaderString& value = entry->value();
  if (strncmp(value.c_str(), kAuthorizationHeaderTokenPrefix.c_str(),
              kAuthorizationHeaderTokenPrefix.length()) != 0) {
    return "AUTHORIZATION_HEADER_BAD_FORMAT";
  }
  Auth::JwtVerifier jwt(value.c_str() +
                        kAuthorizationHeaderTokenPrefix.length());
  if (jwt.GetStatus() != Auth::Status::OK) {
    // Invalid JWT
    return Auth::StatusToString(jwt.GetStatus());
  }

  // Check "exp" claim.
  auto unix_timestamp = std::chrono::duration_cast<std::chrono::seconds>(
                            std::chrono::system_clock::now().time_since_epoch())
                            .count();
  if (jwt.Exp() < unix_timestamp) {
    return "JWT_EXPIRED";
  }

  bool iss_aud_matched = false;
  for (const auto& iss : config_->issuers_) {
    if (iss->failed_ || iss->pkey_->GetStatus() != Auth::Status::OK) {
      continue;
    }
    // Check "iss" claim.
    if (jwt.Iss() != iss->name_) {
      continue;
    }
    if (!iss->IsAudienceAllowed(jwt.Aud())) {
      continue;
    }
    iss_aud_matched = true;

    if (jwt.Verify(*iss->pkey_)) {
      // verification succeeded
      /*
       * TODO: change what to add according to config_->user_info_type_
       */
      headers.addReferenceKey(AuthorizedHeaderKey(), jwt.PayloadStr());

      // Remove JWT from headers.
      headers.remove(kAuthorizationHeaderKey);
      return "OK";
    }
  }
  return iss_aud_matched ? "INVALID_SIGNATURE" : "ISS_AUD_UNMATCH";
}

void JwtVerificationFilter::CompleteVerification(HeaderMap& headers) {
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {}", __func__);
  if (state_ == Responded) {
    // This stream has been reset, abort the callback.
    return;
  }
  std::string status = Verify(headers);
  ENVOY_LOG(debug, "Verification status = {}", status);
  if (status != "OK") {
    // verification failed
    /*
     * TODO: detailed information on message body
     */
    Code code = Code(401);  // Unauthorized
    std::string message_body = "Verification Failed";
    Utility::sendLocalReply(*decoder_callbacks_, false, code, message_body);
    return;
  }

  state_ = Complete;
  if (stopped_) {
    decoder_callbacks_->continueDecoding();
  }
}

}  // Http
}  // Envoy
