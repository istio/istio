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
#ifndef API_MANAGER_REQUEST_HANDLER_H_
#define API_MANAGER_REQUEST_HANDLER_H_

#include "check_workflow.h"
#include "include/api_manager/request_handler_interface.h"
#include "src/api_manager/api_manager_impl.h"
#include "src/api_manager/context/request_context.h"

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
