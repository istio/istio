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
#ifndef API_MANAGER_MOCK_METHOD_INFO_H_
#define API_MANAGER_MOCK_METHOD_INFO_H_

#include "gmock/gmock.h"
#include "include/api_manager/method.h"

namespace google {
namespace api_manager {

class MockMethodInfo : public MethodInfo {
 public:
  virtual ~MockMethodInfo() {}
  MOCK_CONST_METHOD0(name, const std::string&());
  MOCK_CONST_METHOD0(api_name, const std::string&());
  MOCK_CONST_METHOD0(api_version, const std::string&());
  MOCK_CONST_METHOD0(selector, const std::string&());
  MOCK_CONST_METHOD0(auth, bool());
  MOCK_CONST_METHOD0(allow_unregistered_calls, bool());
  MOCK_CONST_METHOD1(isIssuerAllowed, bool(const std::string&));
  MOCK_CONST_METHOD2(isAudienceAllowed,
                     bool(const std::string&, const std::set<std::string>&));
  MOCK_CONST_METHOD1(http_header_parameters,
                     const std::vector<std::string>*(const std::string&));
  MOCK_CONST_METHOD1(url_query_parameters,
                     const std::vector<std::string>*(const std::string&));
  MOCK_CONST_METHOD0(api_key_http_headers, const std::vector<std::string>*());
  MOCK_CONST_METHOD0(api_key_url_query_parameters,
                     const std::vector<std::string>*());
  MOCK_CONST_METHOD0(backend_address, const std::string&());
  MOCK_CONST_METHOD0(rpc_method_full_name, const std::string&());
  MOCK_CONST_METHOD0(request_type_url, const std::string&());
  MOCK_CONST_METHOD0(request_streaming, bool());
  MOCK_CONST_METHOD0(response_type_url, const std::string&());
  MOCK_CONST_METHOD0(response_streaming, bool());
  MOCK_CONST_METHOD0(system_query_parameter_names,
                     const std::set<std::string>&());
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_MOCK_METHOD_INFO_H_
