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
#ifndef API_MANAGER_MOCK_REQUEST_H_
#define API_MANAGER_MOCK_REQUEST_H_

#include "contrib/endpoints/include/api_manager/request.h"
#include "gmock/gmock.h"

namespace google {
namespace api_manager {

class MockRequest : public Request {
 public:
  MOCK_METHOD2(FindQuery, bool(const std::string &, std::string *));
  MOCK_METHOD2(FindHeader, bool(const std::string &, std::string *));
  MOCK_METHOD2(AddHeaderToBackend,
               utils::Status(const std::string &, const std::string &));
  MOCK_METHOD1(SetAuthToken, void(const std::string &));
  MOCK_METHOD0(GetRequestHTTPMethod, std::string());
  MOCK_METHOD0(GetQueryParameters, std::string());
  MOCK_METHOD0(GetFrontendProtocol,
               ::google::api_manager::protocol::Protocol());
  MOCK_METHOD0(GetBackendProtocol, ::google::api_manager::protocol::Protocol());
  MOCK_METHOD0(GetRequestPath, std::string());
  MOCK_METHOD0(GetUnparsedRequestPath, std::string());
  MOCK_METHOD0(GetInsecureCallerID, std::string());
  MOCK_METHOD0(GetClientIP, std::string());
  MOCK_METHOD0(GetRequestHeaders, std::multimap<std::string, std::string> *());
  MOCK_METHOD0(GetGrpcRequestBytes, int64_t());
  MOCK_METHOD0(GetGrpcResponseBytes, int64_t());
  MOCK_METHOD0(GetGrpcRequestMessageCounts, int64_t());
  MOCK_METHOD0(GetGrpcResponseMessageCounts, int64_t());
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_MOCK_REQUEST_H_
