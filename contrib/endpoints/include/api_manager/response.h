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
#ifndef API_MANAGER_RESPONSE_H_
#define API_MANAGER_RESPONSE_H_

#include <map>
#include <string>

#include "contrib/endpoints/include/api_manager/protocol.h"
#include "contrib/endpoints/include/api_manager/service_control.h"
#include "contrib/endpoints/include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// Response provides an interface for CallHandler::StartReport and
// CallHandler::CompleteReport to use to query information about a
// response.
class Response {
 public:
  virtual ~Response() {}

  // Returns the status associated with the response.
  virtual utils::Status GetResponseStatus() = 0;

  // Returns the size of the initial request, in bytes.
  virtual std::size_t GetRequestSize() = 0;

  // Returns the size of the response, in bytes.
  virtual std::size_t GetResponseSize() = 0;

  // Gets the latency info.
  virtual utils::Status GetLatencyInfo(service_control::LatencyInfo *info) = 0;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_RESPONSE_H_
