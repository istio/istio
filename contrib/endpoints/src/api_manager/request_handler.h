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
  RequestHandler(std::shared_ptr<CheckWorkflow> check_workflow,
                 std::shared_ptr<context::ServiceContext> service_context,
                 std::unique_ptr<Request> request_data)
      : context_(new context::RequestContext(service_context,
                                             std::move(request_data))),
        check_workflow_(check_workflow) {}

  virtual ~RequestHandler(){};

  virtual void Check(std::function<void(utils::Status status)> continuation);

  virtual void Report(std::unique_ptr<Response> response,
                      std::function<void(void)> continuation);

  virtual std::string GetServiceConfigId() const;

  virtual void AttemptIntermediateReport();

  virtual std::string GetBackendAddress() const;

  virtual std::string GetRpcMethodFullName() const;

  // Get the method info.
  const MethodInfo *method() const { return context_->method(); }

  // Get the method info.
  const MethodCallInfo *method_call() const { return context_->method_call(); }

 private:
  // The context object needs to pass to the continuation function the check
  // handler as a lambda capture so it can be passed to the next check handler.
  // In order to control the life time of context object, a shared_ptr is used.
  // This object holds a ref_count, the continuation will hold another one.
  std::shared_ptr<context::RequestContext> context_;

  std::shared_ptr<CheckWorkflow> check_workflow_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_REQUEST_HANDLER_H_
