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
#ifndef API_MANAGER_API_MANAGER_IMPL_H_
#define API_MANAGER_API_MANAGER_IMPL_H_

#include "contrib/endpoints/include/api_manager/api_manager.h"
#include "contrib/endpoints/src/api_manager/context/global_context.h"
#include "contrib/endpoints/src/api_manager/context/service_context.h"
#include "contrib/endpoints/src/api_manager/service_control/interface.h"

namespace google {
namespace api_manager {

class CheckWorkflow;

// Implements ApiManager interface.
class ApiManagerImpl : public ApiManager {
 public:
  ApiManagerImpl(std::unique_ptr<ApiManagerEnvInterface> env,
                 const std::string &server_config);

  virtual bool Enabled() const { return service_context_->Enabled(); }

  virtual const std::string &service_name() const {
    return service_context_->service_name();
  }

  virtual const ::google::api::Service &service() const {
    return service_context_->service();
  }

  virtual void SetMetadataServer(const std::string &server) {
    service_context_->SetMetadataServer(server);
  }

  virtual utils::Status SetClientAuthSecret(const std::string &secret) {
    return service_context_->service_account_token()->SetClientAuthSecret(
        secret);
  }

  virtual utils::Status Init() {
    if (service_context_->cloud_trace_aggregator()) {
      service_context_->cloud_trace_aggregator()->Init();
    }
    if (service_control()) {
      return service_control()->Init();
    } else {
      return utils::Status::OK;
    }
  }

  virtual utils::Status Close() {
    if (service_context_->cloud_trace_aggregator()) {
      service_context_->cloud_trace_aggregator()->SendAndClearTraces();
    }
    if (service_control()) {
      return service_control()->Close();
    } else {
      return utils::Status::OK;
    }
  }

  virtual std::unique_ptr<RequestHandlerInterface> CreateRequestHandler(
      std::unique_ptr<Request> request);

  virtual utils::Status GetStatistics(ApiManagerStatistics *statistics) const {
    if (service_control()) {
      return service_control()->GetStatistics(
          &statistics->service_control_statistics);
    } else {
      return utils::Status::OK;
    }
  }

  virtual bool get_logging_status_disabled() {
    return service_context_->DisableLogStatus();
  };

  // Add a new config.
  void AddConfig(std::unique_ptr<Config> config);

 private:
  service_control::Interface *service_control() const {
    return service_context_->service_control();
  }

  // The check work flow.
  std::shared_ptr<CheckWorkflow> check_workflow_;

  // Global context across multiple services.
  std::shared_ptr<context::GlobalContext> global_context_;
  // Service context
  // TODO: will be a map<config_id, ServiceContext>
  // All ServiceContext objects are referring to the same service
  // but different versions. One ESP instance needs to load
  // multiple versions in order to support service rollout automation.
  std::shared_ptr<context::ServiceContext> service_context_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_API_MANAGER_IMPL_H_
