// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////

#include "contrib/endpoints/src/api_manager/check_workflow.h"
#include "contrib/endpoints/src/api_manager/check_auth.h"
#include "contrib/endpoints/src/api_manager/check_security_rules.h"
#include "contrib/endpoints/src/api_manager/check_service_control.h"
#include "contrib/endpoints/src/api_manager/fetch_metadata.h"
#include "contrib/endpoints/src/api_manager/quota_control.h"

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
  // Check Security Rules.
  Register(CheckSecurityRules);
  // Checks service control.
  Register(CheckServiceControl);
  // Quota control
  Register(QuotaControl);
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
