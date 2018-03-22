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
#include "src/istio/authn/context.pb.h"

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

// FilterContext holds inputs, such as request header and connection and
// result data for authentication process.
class FilterContext : public Logger::Loggable<Logger::Id::filter> {
 public:
  FilterContext(HeaderMap* headers, const Network::Connection* connection)
      : headers_(headers), connection_(connection) {}
  virtual ~FilterContext() {}

  // Sets peer result based on authenticated payload. Input payload can be null,
  // which basically changes nothing.
  void setPeerResult(const istio::authn::Payload* payload);

  // Sets origin result based on authenticated payload. Input payload can be
  // null, which basically changes nothing.
  void setOriginResult(const istio::authn::Payload* payload);

  // Sets principal based on binding rule, and the existing peer and origin
  // result.
  void setPrincipal(
      const istio::authentication::v1alpha1::CredentialRule::Binding& binding);

  // Returns the authentication result.
  const istio::authn::Result& authenticationResult() { return result_; }

  // Accessor to headers.
  HeaderMap* headers() { return headers_; }
  // Accessor to connection
  const Network::Connection* connection() { return connection_; }

 private:
  // Pointer to the headers of the request.
  HeaderMap* headers_;

  // Pointer to network connection of the request.
  const Network::Connection* connection_;

  // Holds authentication attribute outputs.
  istio::authn::Result result_;
};

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
