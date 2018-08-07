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
#include "envoy/http/header_map.h"
#include "envoy/json/json_object.h"
#include "src/istio/authn/context.pb.h"

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {

// AuthnUtils class provides utility functions used for authentication.
class AuthnUtils : public Logger::Loggable<Logger::Id::filter> {
 public:
  // Parse JWT payload string (which typically is the output from jwt filter)
  // and populate JwtPayload object. Return true if input string can be parsed
  // successfully. Otherwise, return false.
  static bool ProcessJwtPayload(const std::string& jwt_payload_str,
                                istio::authn::JwtPayload* payload);
};

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
