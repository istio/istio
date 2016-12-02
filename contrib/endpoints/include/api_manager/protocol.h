/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_PROTOCOL_H_
#define API_MANAGER_PROTOCOL_H_

namespace google {
namespace api_manager {

namespace protocol {

enum Protocol { UNKNOWN = 0, HTTP = 1, HTTPS = 2, GRPC = 3 };

inline const char *ToString(Protocol p) {
  switch (p) {
    case HTTP:
      return "http";
    case HTTPS:
      return "https";
    case GRPC:
      return "grpc";
    case UNKNOWN:
    default:
      return "unknown";
  }
}

}  // namespace protocol

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_PROTOCOL_H_
