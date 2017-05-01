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
//
#include "contrib/endpoints/src/api_manager/context/service_context.h"
#include "contrib/endpoints/src/api_manager/service_control/aggregated.h"

namespace google {
namespace api_manager {
namespace context {

namespace {

const char kHTTPHeadMethod[] = "HEAD";
const char kHTTPGetMethod[] = "GET";
}

ServiceContext::ServiceContext(std::shared_ptr<GlobalContext> global_context,
                               std::unique_ptr<Config> config)
    : global_context_(global_context),
      config_(std::move(config)),
      service_control_(CreateInterface()) {
  config_->set_server_config(global_context_->server_config());
}

ServiceContext::ServiceContext(std::unique_ptr<ApiManagerEnvInterface> env,
                               const std::string& server_config,
                               std::unique_ptr<Config> config)
    : ServiceContext(
          std::make_shared<GlobalContext>(std::move(env), server_config),
          std::move(config)) {}

MethodCallInfo ServiceContext::GetMethodCallInfo(
    const std::string& http_method, const std::string& url,
    const std::string& query_params) const {
  if (config_ == nullptr) {
    return MethodCallInfo();
  }
  MethodCallInfo method_call_info =
      config_->GetMethodCallInfo(http_method, url, query_params);
  // HEAD should be treated as GET unless it is specified from service_config.
  if (method_call_info.method_info == nullptr &&
      http_method == kHTTPHeadMethod) {
    method_call_info =
        config_->GetMethodCallInfo(kHTTPGetMethod, url, query_params);
  }
  return method_call_info;
}

const std::string& ServiceContext::project_id() const {
  if (!global_context_->project_id().empty()) {
    return global_context_->project_id();
  } else {
    return config_->service().producer_project_id();
  }
}

std::unique_ptr<service_control::Interface> ServiceContext::CreateInterface() {
  return std::unique_ptr<service_control::Interface>(
      service_control::Aggregated::Create(
          config_->service(), global_context_->server_config().get(), env(),
          global_context_->service_account_token()));
}

}  // namespace context
}  // namespace api_manager
}  // namespace google
