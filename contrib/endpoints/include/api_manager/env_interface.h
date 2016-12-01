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
#ifndef API_MANAGER_ENV_INTERFACE_H_
#define API_MANAGER_ENV_INTERFACE_H_

// An interface for the API Manager to access its environment.

#include <chrono>
#include <functional>
#include <memory>

#include "include/api_manager/http_request.h"
#include "include/api_manager/periodic_timer.h"
#include "include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

class ApiManagerEnvInterface {
 public:
  virtual ~ApiManagerEnvInterface() {}

  enum LogLevel { DEBUG, INFO, WARNING, ERROR };

  void LogDebug(const std::string &str) { LogDebug(str.c_str()); }
  void LogInfo(const std::string &str) { LogInfo(str.c_str()); }
  void LogWarning(const std::string &str) { LogWarning(str.c_str()); }
  void LogError(const std::string &str) { LogError(str.c_str()); }

  void LogDebug(const char *message) { Log(DEBUG, message); }
  void LogInfo(const char *message) { Log(INFO, message); }
  void LogWarning(const char *message) { Log(WARNING, message); }
  void LogError(const char *message) { Log(ERROR, message); }

  virtual void Log(LogLevel level, const char *message) = 0;

  // Simple periodic timer support. API Manager uses this method to get
  // called at regular intervals of wall-clock time.
  virtual std::unique_ptr<PeriodicTimer> StartPeriodicTimer(
      std::chrono::milliseconds interval,
      std::function<void()> continuation) = 0;

  // Environment support for issuing non-streaming HTTP 1.1 requests from
  // the API manager. The environment takes ownership of the request, and is
  // responsible for eventually invoking request->OnComplete() with the result
  // of the request
  // (possibly before returning).
  virtual void RunHTTPRequest(std::unique_ptr<HTTPRequest> request) = 0;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_ENV_INTERFACE_H_
