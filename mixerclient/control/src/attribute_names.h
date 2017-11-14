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

#ifndef MIXERCONTROL_ATTRIBUTE_NAMES_H
#define MIXERCONTROL_ATTRIBUTE_NAMES_H

#include <string>

namespace istio {
namespace mixer_control {

// Define attribute names
struct AttributeName {
  static const char kSourceUser[];

  static const char kRequestHeaders[];
  static const char kRequestHost[];
  static const char kRequestMethod[];
  static const char kRequestPath[];
  static const char kRequestReferer[];
  static const char kRequestScheme[];
  static const char kRequestSize[];
  static const char kRequestTime[];
  static const char kRequestUserAgent[];

  static const char kResponseCode[];
  static const char kResponseDuration[];
  static const char kResponseHeaders[];
  static const char kResponseSize[];
  static const char kResponseTime[];

  // TCP attributes
  // Downstream tcp connection: source ip/port.
  static const char kSourceIp[];
  static const char kSourcePort[];
  // Upstream tcp connection: destionation ip/port.

  static const char kDestinationIp[];
  static const char kDestinationPort[];
  static const char kConnectionReceviedBytes[];
  static const char kConnectionReceviedTotalBytes[];
  static const char kConnectionSendBytes[];
  static const char kConnectionSendTotalBytes[];
  static const char kConnectionDuration[];

  // Context attributes
  static const char kContextProtocol[];
  static const char kContextTime[];

  // Check status code.
  static const char kCheckStatusCode[];
};

}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_ATTRIBUTE_NAMES_H
