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
#ifndef API_MANAGER_MOCK_ESP_ENVIRONMENT_H_
#define API_MANAGER_MOCK_ESP_ENVIRONMENT_H_

#include "gmock/gmock.h"
#include "include/api_manager/api_manager.h"

namespace google {
namespace api_manager {

class MockApiManagerEnvironment : public ApiManagerEnvInterface {
 public:
  MOCK_METHOD2(Log, void(LogLevel, const char *));
  MOCK_METHOD1(MakeTag, void *(std::function<void(bool)>));
  MOCK_METHOD2(StartPeriodicTimer,
               std::unique_ptr<PeriodicTimer>(std::chrono::milliseconds,
                                              std::function<void()>));
  MOCK_METHOD1(DoRunHTTPRequest, void(HTTPRequest *));
  virtual void RunHTTPRequest(std::unique_ptr<HTTPRequest> req) {
    DoRunHTTPRequest(req.get());
  }
};

// A useful mock class to log to stdout to debug Config loading failure.
// Replace
//    ::testing::NiceMock<MockApiManagerEnvironment> env;
// With
//    MockApiManagerEnvironmentWithLog env;
class MockApiManagerEnvironmentWithLog : public ApiManagerEnvInterface {
 public:
  void Log(LogLevel level, const char *message) {
    std::cout << "LOG: " << message << std::endl;
  }
  void *MakeTag(std::function<void(bool)>) { return nullptr; }
  std::unique_ptr<PeriodicTimer> StartPeriodicTimer(std::chrono::milliseconds,
                                                    std::function<void()>) {
    return std::unique_ptr<PeriodicTimer>();
  }
  void RunHTTPRequest(std::unique_ptr<HTTPRequest> request) {
    std::map<std::string, std::string> headers;
    request->OnComplete(utils::Status::OK, std::move(headers), "");
  }
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_MOCK_ESP_ENVIRONMENT_H_
