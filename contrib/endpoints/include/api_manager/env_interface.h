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
