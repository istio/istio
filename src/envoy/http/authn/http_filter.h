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

#pragma once

#include "authentication/v1alpha1/policy.pb.h"
#include "common/common/logger.h"
#include "server/config/network/http_connection_manager.h"

namespace Envoy {
namespace Http {

// The authentication filter.
class AuthenticationFilter : public StreamDecoderFilter,
                             public Logger::Loggable<Logger::Id::filter> {
 public:
  AuthenticationFilter(const istio::authentication::v1alpha1::Policy& config);
  ~AuthenticationFilter();

  // Http::StreamFilterBase
  void onDestroy() override;

  // Http::StreamDecoderFilter
  FilterHeadersStatus decodeHeaders(HeaderMap& headers, bool) override;
  FilterDataStatus decodeData(Buffer::Instance&, bool) override;
  FilterTrailersStatus decodeTrailers(HeaderMap&) override;
  void setDecoderFilterCallbacks(
      StreamDecoderFilterCallbacks& callbacks) override;

 private:
  // Store the config.
  const istio::authentication::v1alpha1::Policy& config_;
  StreamDecoderFilterCallbacks* decoder_callbacks_;
};

}  // namespace Http
}  // namespace Envoy
