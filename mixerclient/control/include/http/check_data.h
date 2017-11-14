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

#ifndef MIXERCONTROL_HTTP_CHECK_DATA_H
#define MIXERCONTROL_HTTP_CHECK_DATA_H

#include <map>
#include <string>

namespace istio {
namespace mixer_control {
namespace http {

// The interface to extract HTTP data for Mixer check.
// Implemented by the environment (Envoy) and used by the library.
class CheckData {
 public:
  virtual ~CheckData() {}

  // Find "x-istio-attributes" HTTP header.
  // If found, base64 decode its value,  pass it out
  // and remove the HTTP header from the request.
  virtual bool ExtractIstioAttributes(std::string* data) = 0;

  // Base64 encode data, and add it as "x-istio-attributes" HTTP header.
  virtual void AddIstioAttributes(const std::string& data) = 0;

  // Get downstream tcp connection ip and port.
  virtual bool GetSourceIpPort(std::string* ip, int* port) const = 0;

  // If SSL is used, get origin user name.
  virtual bool GetSourceUser(std::string* user) const = 0;

  // Get request HTTP headers
  virtual std::map<std::string, std::string> GetRequestHeaders() const = 0;

  // These headers are extracted into top level attributes.
  // They can be retrieved at O(1) speed by Platform (Envoy).
  // It is faster to use the map from GetRequestHeader() call.
  enum HeaderType {
    HEADER_PATH = 0,
    HEADER_HOST,
    HEADER_SCHEME,
    HEADER_USER_AGENT,
    HEADER_METHOD,
    HEADER_REFERER,
  };
  virtual bool FindRequestHeader(HeaderType header_type,
                                 std::string* value) const = 0;
};

}  // namespace http
}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_HTTP_CHECK_DATA_H
