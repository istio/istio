/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
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
