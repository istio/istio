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
#ifndef API_MANAGER_REQUEST_HANDLER_H_
#define API_MANAGER_REQUEST_HANDLER_H_

#include "check_workflow.h"
#include "contrib/endpoints/include/api_manager/request_handler_interface.h"
#include "contrib/endpoints/src/api_manager/api_manager_impl.h"
#include "contrib/endpoints/src/api_manager/context/request_context.h"

namespace google {
namespace api_manager {

// Implements RequestHandlerInterface.
class RequestHandler : public RequestHandlerInterface {
 public:
  RequestHandler(ApiManagerImpl* api_manager,
                 std::shared_ptr<CheckWorkflow> check_workflow,
                 std::unique_ptr<Request> request_data)
      : api_manager_(api_manager),
        check_workflow_(check_workflow),
        request_data_(std::move(request_data)) {}

  virtual ~RequestHandler(){};

  virtual void Check(std::function<void(utils::Status status)> continuation);

  virtual void Report(std::unique_ptr<Response> response,
                      std::function<void(void)> continuation);

  virtual std::string GetServiceConfigId() const;

  virtual void AttemptIntermediateReport();

  virtual std::string GetBackendAddress() const;

  virtual std::string GetRpcMethodFullName() const;

  // Get the method info.
  const MethodInfo* method() const { return context_->method(); }

  // Get the method info.
  const MethodCallInfo* method_call() const { return context_->method_call(); }

 private:
  // Create a context_ from ApiManager. Return true if the RequestContext was
  // successfully created
  bool CreateRequestContext();
  // Internal Check
  void InternalCheck(std::function<void(utils::Status status)> continuation);
  // Internal Report
  void InternalReport(std::unique_ptr<Response> response,
                      std::function<void(void)> continuation);
  // ApiManager instance
  ApiManagerImpl* api_manager_;

  // The context object needs to pass to the continuation function the check
  // handler as a lambda capture so it can be passed to the next check handler.
  // In order to control the life time of context object, a shared_ptr is used.
  // This object holds a ref_count, the continuation will hold another one.
  std::shared_ptr<context::RequestContext> context_;

  std::shared_ptr<CheckWorkflow> check_workflow_;
  // Unique copy of the request data to initialize context_ later
  std::unique_ptr<Request> request_data_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_REQUEST_HANDLER_H_
