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

#include "src/api_manager/check_workflow.h"
#include "src/api_manager/check_auth.h"
#include "src/api_manager/check_service_control.h"
#include "src/api_manager/fetch_metadata.h"

using ::google::api_manager::utils::Status;

namespace google {
namespace api_manager {

void CheckWorkflow::RegisterAll() {
  // Fetchs GCE metadata.
  Register(FetchGceMetadata);
  // Fetchs service account token.
  Register(FetchServiceAccountToken);
  // Authentication checks.
  Register(CheckAuth);
  // Checks service control.
  Register(CheckServiceControl);
}

void CheckWorkflow::Register(CheckHandler handler) {
  handlers_.push_back(handler);
}

void CheckWorkflow::Run(std::shared_ptr<context::RequestContext> context) {
  if (!handlers_.empty()) {
    RunOneHandler(context, 0);
  } else {
    // Empty check handler list means: not need to check.
    context->CompleteCheck(Status::OK);
  }
}

void CheckWorkflow::RunOneHandler(
    std::shared_ptr<context::RequestContext> context, size_t index) {
  handlers_[index](context, [context, index, this](Status status) {
    if (status.ok() && index + 1 < handlers_.size()) {
      RunOneHandler(context, index + 1);
    } else {
      context->CompleteCheck(status);
    }
  });
}

}  // namespace api_manager
}  // namespace google
