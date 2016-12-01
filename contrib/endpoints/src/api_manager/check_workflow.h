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
#ifndef API_MANAGER_CHECK_WORKFLOW_H_
#define API_MANAGER_CHECK_WORKFLOW_H_

#include "include/api_manager/utils/status.h"
#include "src/api_manager/context/request_context.h"

namespace google {
namespace api_manager {

// The prototype for CheckHandler
typedef std::function<void(std::shared_ptr<context::RequestContext>,
                           std::function<void(utils::Status)>)>
    CheckHandler;

// A workflow to run all CheckHandlers
class CheckWorkflow {
 public:
  virtual ~CheckWorkflow() {}

  // Registers all known check handlers.
  void RegisterAll();

  // Runs the workflow to call each check handler sequentially.
  void Run(std::shared_ptr<context::RequestContext> context);

 private:
  // Registers a check handler. The order is important.
  // They will be executed in the order they are registered.
  void Register(CheckHandler handler);

  // Runs one check handler with index.
  void RunOneHandler(std::shared_ptr<context::RequestContext> context,
                     size_t index);

  // A vector to store all check handlers.
  std::vector<CheckHandler> handlers_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_CHECK_WORKFLOW_H_
