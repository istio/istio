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

#include "proxy/src/envoy/http/jwt_auth/http_filter.h"

#include "common/http/message_impl.h"
#include "common/http/utility.h"
#include "envoy/http/async_client.h"
#include "proxy/src/envoy/utils/filter_names.h"

#include <chrono>
#include <string>

namespace Envoy {
namespace Http {

JwtVerificationFilter::JwtVerificationFilter(Upstream::ClusterManager& cm,
                                             JwtAuth::JwtAuthStore& store)
    : jwt_auth_(cm, store) {}

JwtVerificationFilter::~JwtVerificationFilter() {}

void JwtVerificationFilter::onDestroy() { jwt_auth_.onDestroy(); }

FilterHeadersStatus JwtVerificationFilter::decodeHeaders(HeaderMap& headers,
                                                         bool) {
  state_ = Calling;
  stopped_ = false;

  // Verify the JWT token, onDone() will be called when completed.
  jwt_auth_.Verify(headers, this);

  if (state_ == Complete) {
    return FilterHeadersStatus::Continue;
  }
  ENVOY_LOG(debug, "Called JwtVerificationFilter : {} Stop", __func__);
  stopped_ = true;
  return FilterHeadersStatus::StopIteration;
}

void JwtVerificationFilter::onDone(const JwtAuth::Status& status) {
  ENVOY_LOG(debug, "JwtVerificationFilter::onDone with status {}",
            JwtAuth::StatusToString(status));
  // This stream has been reset, abort the callback.
  if (state_ == Responded) {
    return;
  }
  if (status != JwtAuth::Status::OK) {
    state_ = Responded;
    // verification failed
    Code code = Code(401);  // Unauthorized
    // return failure reason as message body
    decoder_callbacks_->sendLocalReply(code, JwtAuth::StatusToString(status),
                                       nullptr);
    return;
  }

  state_ = Complete;
  if (stopped_) {
    decoder_callbacks_->continueDecoding();
  }
}

void JwtVerificationFilter::savePayload(const std::string& key,
                                        const std::string& payload) {
  decoder_callbacks_->requestInfo().setDynamicMetadata(
      Utils::IstioFilterName::kJwt, MessageUtil::keyValueStruct(key, payload));
}

FilterDataStatus JwtVerificationFilter::decodeData(Buffer::Instance&, bool) {
  if (state_ == Calling) {
    return FilterDataStatus::StopIterationAndWatermark;
  }
  return FilterDataStatus::Continue;
}

FilterTrailersStatus JwtVerificationFilter::decodeTrailers(HeaderMap&) {
  if (state_ == Calling) {
    return FilterTrailersStatus::StopIteration;
  }
  return FilterTrailersStatus::Continue;
}

void JwtVerificationFilter::setDecoderFilterCallbacks(
    StreamDecoderFilterCallbacks& callbacks) {
  decoder_callbacks_ = &callbacks;
}

}  // namespace Http
}  // namespace Envoy
