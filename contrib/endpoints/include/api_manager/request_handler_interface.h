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
