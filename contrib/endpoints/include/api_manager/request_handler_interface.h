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
#ifndef API_MANAGER_REQUEST_HANDLER_INTERFACE_H_
#define API_MANAGER_REQUEST_HANDLER_INTERFACE_H_

#include "include/api_manager/method.h"
#include "include/api_manager/method_call_info.h"
#include "include/api_manager/response.h"
#include "include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// Defines an interface for API Manager to handle a request.
// Callers call API Manager to create such interface, and call
// its Check() function to process all checking, and call
// its Report() function to send a report.
class RequestHandlerInterface {
 public:
  virtual ~RequestHandlerInterface(){};

  // Makes all necessary checks for a request,
  // All request data can be accessed from request object.
  // continuation will be called once it is done or failed.
  virtual void Check(
      std::function<void(utils::Status status)> continuation) = 0;

  // Sends a report.
  virtual void Report(std::unique_ptr<Response> response,
                      std::function<void(void)> continuation) = 0;

  // Attempt to send intermediate report in streaming calls.
  virtual void AttemptIntermediateReport() = 0;

  // Get the backend address.
  virtual std::string GetBackendAddress() const = 0;

  // Get the full name of the RPC method in the "/<API name>/<method name>"
  // form.
  virtual std::string GetRpcMethodFullName() const = 0;

  // Get the method info.
  virtual const MethodInfo *method() const = 0;

  // Get the method info.
  virtual const MethodCallInfo *method_call() const = 0;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_REQUEST_HANDLER_INTERFACE_H_
