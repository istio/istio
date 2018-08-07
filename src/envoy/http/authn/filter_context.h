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
#include "envoy/api/v2/core/base.pb.h"
#include "envoy/config/filter/http/authn/v2alpha1/config.pb.h"
#include "envoy/network/connection.h"
#include "src/istio/authn/context.pb.h"

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

// FilterContext holds inputs, such as request dynamic metadata and connection
// and result data for authentication process.
class FilterContext : public Logger::Loggable<Logger::Id::filter> {
 public:
  FilterContext(
      const envoy::api::v2::core::Metadata& dynamic_metadata,
      const Network::Connection* connection,
      const istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig&
          filter_config)
      : dynamic_metadata_(dynamic_metadata),
        connection_(connection),
        filter_config_(filter_config) {}
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
      const istio::authentication::v1alpha1::PrincipalBinding& binding);

  // Returns the authentication result.
  const istio::authn::Result& authenticationResult() { return result_; }

  // Accessor to connection
  const Network::Connection* connection() { return connection_; }
  // Accessor to the filter config
  const istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig&
  filter_config() const {
    return filter_config_;
  }

  // Gets JWT payload (output from JWT filter) for given issuer. If non-empty
  // payload found, returns true and set the output payload string. Otherwise,
  // returns false.
  bool getJwtPayload(const std::string& issuer, std::string* payload) const;

 private:
  // Const reference to request info dynamic metadata. This provides data that
  // output from other filters, e.g JWT.
  const envoy::api::v2::core::Metadata& dynamic_metadata_;

  // Pointer to network connection of the request.
  const Network::Connection* connection_;

  // Holds authentication attribute outputs.
  istio::authn::Result result_;

  // Store the Istio authn filter config.
  const istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig&
      filter_config_;
};

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
