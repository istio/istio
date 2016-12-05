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
#ifndef API_MANAGER_METHOD_CALL_INFO_H_
#define API_MANAGER_METHOD_CALL_INFO_H_

#include <string>
#include <vector>

#include "contrib/endpoints/include/api_manager/method.h"

namespace google {
namespace api_manager {

// VariableBinding specifies a value for a single field in the request message.
// When transcoding HTTP/REST/JSON to gRPC/proto the request message is
// constructed using the HTTP body and the variable bindings (specified through
// request url).
struct VariableBinding {
  // The location of the field in the protobuf message, where the value
  // needs to be inserted, e.g. "shelf.theme" would mean the "theme" field
  // of the nested "shelf" message of the request protobuf message.
  std::vector<std::string> field_path;
  // The value to be inserted.
  std::string value;
};

// Information that we store per each call of a method. It includes information
// about the method itself and variable bindings for the particular call.
struct MethodCallInfo {
  // Method information
  const MethodInfo* method_info;
  // Variable bindings
  std::vector<VariableBinding> variable_bindings;
  // Body prefix (the field of the message where the HTTP body should go)
  std::string body_field_path;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_METHOD_CALL_INFO_H_
