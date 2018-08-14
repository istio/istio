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

#include "common/common/logger.h"
#include "envoy/config/filter/http/authn/v2alpha1/config.pb.h"
#include "envoy/http/filter.h"
#include "proxy/src/envoy/http/authn/authenticator_base.h"
#include "proxy/src/envoy/http/authn/filter_context.h"

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

// The authentication filter.
class AuthenticationFilter : public StreamDecoderFilter,
                             public Logger::Loggable<Logger::Id::filter> {
 public:
  AuthenticationFilter(
      const istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig&
          config);
  ~AuthenticationFilter();

  // Http::StreamFilterBase
  void onDestroy() override;

  // Http::StreamDecoderFilter
  FilterHeadersStatus decodeHeaders(HeaderMap& headers, bool) override;
  FilterDataStatus decodeData(Buffer::Instance&, bool) override;
  FilterTrailersStatus decodeTrailers(HeaderMap&) override;
  void setDecoderFilterCallbacks(
      StreamDecoderFilterCallbacks& callbacks) override;

 protected:
  // Convenient function to call decoder_callbacks_ only when stopped_ is true.
  void continueDecoding();

  // Convenient function to reject request.
  void rejectRequest(const std::string& message);

  // Creates peer authenticator. This is made virtual function for
  // testing.
  virtual std::unique_ptr<Istio::AuthN::AuthenticatorBase>

  createPeerAuthenticator(Istio::AuthN::FilterContext* filter_context);

  // Creates origin authenticator.
  virtual std::unique_ptr<Istio::AuthN::AuthenticatorBase>

  createOriginAuthenticator(Istio::AuthN::FilterContext* filter_context);

 private:
  // Store the config.
  const istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig&
      filter_config_;

  StreamDecoderFilterCallbacks* decoder_callbacks_{};

  enum State { INIT, PROCESSING, COMPLETE, REJECTED };
  // Holds the state of the filter.
  State state_{State::INIT};

  // Context for authentication process. Created in decodeHeader to start
  // authentication process.
  std::unique_ptr<Istio::AuthN::FilterContext> filter_context_;
};

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
