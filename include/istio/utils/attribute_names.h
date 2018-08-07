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

#ifndef ISTIO_UTILS_ATTRIBUTE_NAMES_H
#define ISTIO_UTILS_ATTRIBUTE_NAMES_H

#include <string>

namespace istio {
namespace utils {

// Define attribute names
struct AttributeName {
  // source.user is replaced by source.principal
  // https://github.com/istio/istio/issues/4689
  static const char kSourceUser[];
  static const char kSourcePrincipal[];
  static const char kDestinationPrincipal[];

  static const char kRequestHeaders[];
  static const char kRequestHost[];
  static const char kRequestMethod[];
  static const char kRequestPath[];
  static const char kRequestReferer[];
  static const char kRequestScheme[];
  static const char kRequestBodySize[];
  // Total size of request received, including request headers, body, and
  // trailers.
  static const char kRequestTotalSize[];
  static const char kRequestTime[];
  static const char kRequestUserAgent[];
  static const char kRequestApiKey[];

  static const char kResponseCode[];
  static const char kResponseDuration[];
  static const char kResponseHeaders[];
  static const char kResponseBodySize[];
  // Total size of response sent, including response headers and body.
  static const char kResponseTotalSize[];
  static const char kResponseTime[];

  // TCP attributes
  // Downstream tcp connection: source ip/port.
  static const char kSourceIp[];
  static const char kSourcePort[];
  // Upstream tcp connection: destionation ip/port.

  static const char kDestinationIp[];
  static const char kDestinationPort[];
  static const char kDestinationUID[];
  static const char kOriginIp[];
  static const char kConnectionReceviedBytes[];
  static const char kConnectionReceviedTotalBytes[];
  static const char kConnectionSendBytes[];
  static const char kConnectionSendTotalBytes[];
  static const char kConnectionDuration[];
  static const char kConnectionMtls[];
  static const char kConnectionRequestedServerName[];
  static const char kConnectionId[];
  // Record TCP connection status: open, continue, close
  static const char kConnectionEvent[];

  // Context attributes
  static const char kContextProtocol[];
  static const char kContextTime[];

  // Check error code and message.
  static const char kCheckErrorCode[];
  static const char kCheckErrorMessage[];

  // Check and Quota cache hit
  static const char kCheckCacheHit[];
  static const char kQuotaCacheHit[];

  // Authentication attributes
  static const char kRequestAuthPrincipal[];
  static const char kRequestAuthAudiences[];
  static const char kRequestAuthGroups[];
  static const char kRequestAuthPresenter[];
  static const char kRequestAuthClaims[];
  static const char kRequestAuthRawClaims[];

  static const char kResponseGrpcStatus[];
  static const char kResponseGrpcMessage[];
};

}  // namespace utils
}  // namespace istio

#endif  // ISTIO_UTILS_ATTRIBUTE_NAMES_H
