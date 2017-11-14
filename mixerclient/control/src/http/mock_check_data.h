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

#ifndef MIXERCONTROL_HTTP_MOCK_CHECK_DATA_H
#define MIXERCONTROL_HTTP_MOCK_CHECK_DATA_H

#include "control/include/http/check_data.h"
#include "gmock/gmock.h"

namespace istio {
namespace mixer_control {
namespace http {

// The mock object for CheckData interface.
class MockCheckData : public CheckData {
 public:
  MOCK_METHOD1(ExtractIstioAttributes, bool(std::string* data));
  MOCK_METHOD1(AddIstioAttributes, void(const std::string& data));

  MOCK_CONST_METHOD2(GetSourceIpPort, bool(std::string* ip, int* port));
  MOCK_CONST_METHOD1(GetSourceUser, bool(std::string* user));
  MOCK_CONST_METHOD0(GetRequestHeaders, std::map<std::string, std::string>());
  MOCK_CONST_METHOD2(FindRequestHeader,
                     bool(HeaderType header_type, std::string* value));
};

}  // namespace http
}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_HTTP_MOCK_CHECK_DATA_H
