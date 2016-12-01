// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/api_manager_impl.h"

#include "src/api_manager/check_workflow.h"
#include "src/api_manager/request_handler.h"

using ::google::api_manager::proto::ServerConfig;

namespace google {
namespace api_manager {

namespace {

std::shared_ptr<ApiManager> CreateApiManager(
    std::unique_ptr<ApiManagerEnvInterface> env,
    std::unique_ptr<Config> config) {
  return std::shared_ptr<ApiManager>(
      new ApiManagerImpl(std::move(env), std::move(config)));
}

}  // namespace

ApiManagerImpl::ApiManagerImpl(std::unique_ptr<ApiManagerEnvInterface> env,
                               std::unique_ptr<Config> config)
    : service_context_(
          new context::ServiceContext(std::move(env), std::move(config))) {
  check_workflow_ = std::unique_ptr<CheckWorkflow>(new CheckWorkflow);
  check_workflow_->RegisterAll();
}

std::unique_ptr<RequestHandlerInterface> ApiManagerImpl::CreateRequestHandler(
    std::unique_ptr<Request> request_data) {
  return std::unique_ptr<RequestHandlerInterface>(new RequestHandler(
      check_workflow_, service_context_, std::move(request_data)));
}

std::shared_ptr<ApiManager> ApiManagerFactory::GetOrCreateApiManager(
    std::unique_ptr<ApiManagerEnvInterface> env,
    const std::string& service_config, const std::string& server_config) {
  std::unique_ptr<Config> config =
      Config::Create(env.get(), service_config, server_config);
  if (config == nullptr) {
    return nullptr;
  }

  ApiManagerMap::iterator it;
  std::tie(it, std::ignore) = api_manager_map_.emplace(
      config->service_name(), std::weak_ptr<ApiManager>());
  std::shared_ptr<ApiManager> result = it->second.lock();

  if (!result) {
    // TODO: Handle the case where the caller gives us a different
    // config with the same service name.
    result = CreateApiManager(std::move(env), std::move(config));
    it->second = result;
  }

  return result;
}

}  // namespace api_manager
}  // namespace google
