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

#ifndef ISTIO_CONTROL_HTTP_MOCK_CHECK_DATA_H
#define ISTIO_CONTROL_HTTP_MOCK_CHECK_DATA_H

#include "gmock/gmock.h"
#include "google/protobuf/struct.pb.h"
#include "proxy/include/istio/control/http/check_data.h"

namespace istio {
namespace control {
namespace http {

// The mock object for CheckData interface.
class MockCheckData : public CheckData {
 public:
  MOCK_CONST_METHOD1(ExtractIstioAttributes, bool(std::string *data));

  MOCK_CONST_METHOD2(GetSourceIpPort, bool(std::string *ip, int *port));
  MOCK_CONST_METHOD2(GetPrincipal, bool(bool peer, std::string *user));
  MOCK_CONST_METHOD0(GetRequestHeaders, std::map<std::string, std::string>());
  MOCK_CONST_METHOD2(FindHeaderByType,
                     bool(HeaderType header_type, std::string *value));
  MOCK_CONST_METHOD2(FindHeaderByName,
                     bool(const std::string &name, std::string *value));
  MOCK_CONST_METHOD2(FindQueryParameter,
                     bool(const std::string &name, std::string *value));
  MOCK_CONST_METHOD2(FindCookie,
                     bool(const std::string &name, std::string *value));
  MOCK_CONST_METHOD1(GetJWTPayload,
                     bool(std::map<std::string, std::string> *payload));
  MOCK_CONST_METHOD0(GetAuthenticationResult,
                     const ::google::protobuf::Struct *());
  MOCK_CONST_METHOD0(IsMutualTLS, bool());
  MOCK_CONST_METHOD1(GetRequestedServerName, bool(std::string *name));
  MOCK_CONST_METHOD1(GetUrlPath, bool(std::string *));
  MOCK_CONST_METHOD1(GetRequestQueryParams,
                     bool(std::map<std::string, std::string> *));
};

// The mock object for HeaderUpdate interface.
class MockHeaderUpdate : public HeaderUpdate {
 public:
  MOCK_METHOD0(RemoveIstioAttributes, void());
  MOCK_METHOD1(AddIstioAttributes, void(const std::string &data));
};

}  // namespace http
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_HTTP_MOCK_CHECK_DATA_H
