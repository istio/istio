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
#include "contrib/endpoints/src/api_manager/config_manager.h"
#include "contrib/endpoints/src/api_manager/context/global_context.h"
#include "contrib/endpoints/src/api_manager/context/service_context.h"
#include "contrib/endpoints/src/api_manager/service_control/interface.h"
#include "contrib/endpoints/src/api_manager/weighted_selector.h"

namespace google {
namespace api_manager {

class CheckWorkflow;

// Implements ApiManager interface.
class ApiManagerImpl : public ApiManager {
 public:
  ApiManagerImpl(std::unique_ptr<ApiManagerEnvInterface> env,
                 const std::string &server_config);

  bool Enabled() const override;

  const std::string &service_name() const override;
  const ::google::api::Service &service(
      const std::string &config_id) const override;

  utils::Status Init() override;
  utils::Status Close() override;

  std::unique_ptr<RequestHandlerInterface> CreateRequestHandler(
      std::unique_ptr<Request> request) override;

  bool get_logging_status_disabled() override {
    return global_context_->DisableLogStatus();
  };

  utils::Status GetStatistics(ApiManagerStatistics *statistics) const override;

  // Add a new service config.
  // Return true if service_config is valid, otherwise return false.
  // config_id will be updated when the deployment was successful
  utils::Status AddConfig(const std::string &service_config, bool initialize,
                          std::string *config_id);

  // Return ServiceContext for selected by WeightedSelector
  std::shared_ptr<context::ServiceContext> SelectService();

  // Load service rollouts. This can be called only once, the data is from
  // server_config.
  utils::Status LoadServiceRollouts() override;

 private:
  // Use these configs according to the traffic percentage.
  void DeployConfigs(std::vector<std::pair<std::string, int>> &&list);

  // Add and deploy service configs. Return utils::Status::OK when everything
  // is ok.
  utils::Status AddAndDeployConfigs(
      std::vector<std::pair<std::string, int>> &&configs, bool initialize);

  // The check work flow.
  std::shared_ptr<CheckWorkflow> check_workflow_;

  // Global context across multiple services.
  std::shared_ptr<context::GlobalContext> global_context_;

  // Service context map
  // All ServiceContext objects are referring to the same service
  // but different versions. One ESP instance needs to load
  // multiple versions in order to support service rollout automation.
  std::map<std::string, std::shared_ptr<context::ServiceContext>>
      service_context_map_;

  // A weighted service selector.
  std::unique_ptr<WeightedSelector> service_selector_;

  // A config manager will be initialized when server_config.rollout_strategy is
  // set to "managed"
  std::unique_ptr<ConfigManager> config_manager_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_API_MANAGER_IMPL_H_
