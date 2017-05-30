/* Copyright 2017 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_CONFIG_MANAGER_H_
#define API_MANAGER_CONFIG_MANAGER_H_

#include "contrib/endpoints/src/api_manager/context/global_context.h"
#include "contrib/endpoints/src/api_manager/service_management_fetch.h"

namespace google {
namespace api_manager {

namespace {

// RolloutApplyFunction is the callback provided by ApiManager.
// ConfigManager calls the callback after the service config download
//
// status
//  - Code::OK        Config manager was successfully initialized
//  - Code::ABORTED   Fatal error
//  - Code::UNKNOWN   Config manager was not initialized yet
// configs - pairs of ServiceConfig in text and rollout percentages
typedef std::function<void(
    const utils::Status& status,
    const std::vector<std::pair<std::string, int>>& configs)>
    RolloutApplyFunction;

// Data structure to fetch configs from rollouts
struct ConfigsFetchInfo {
  ConfigsFetchInfo() : finished(0) {}

  ConfigsFetchInfo(std::vector<std::pair<std::string, int>>&& rollouts)
      : rollouts(std::move(rollouts)), finished(0) {}

  // config_ids to be fetched and rollouts percentages
  std::vector<std::pair<std::string, int>> rollouts;
  // fetched ServiceConfig and rollouts percentages
  std::vector<std::pair<std::string, int>> configs;
  // Finished fetching
  inline bool IsCompleted() { return ((size_t)finished == rollouts.size()); }
  // Check fetched rollout is empty
  inline bool IsRolloutsEmpty() { return rollouts.empty(); }
  // Check fetched configs are empty
  inline bool IsConfigsEmpty() { return configs.empty(); }

  // Finished service config fetch count
  int finished;
};

}  // namespace anonymous

// Manages configuration downloading
class ConfigManager {
 public:
  ConfigManager(std::shared_ptr<context::GlobalContext> global_context,
                RolloutApplyFunction config_rollout_callback);
  virtual ~ConfigManager(){};

 public:
  // Initialize the instance
  void Init();

 private:
  // Fetch ServiceConfig details from the latest successful rollouts
  // https://goo.gl/I2nD4M
  void FetchConfigs(std::shared_ptr<ConfigsFetchInfo> config_fetch_info);
  // Handle metadata fetch done
  void OnFetchMetadataDone(utils::Status status);
  // Handle auth token fetch done
  void OnFetchAuthTokenDone(utils::Status status);

  // Global context provided by ApiManager
  std::shared_ptr<context::GlobalContext> global_context_;
  // ApiManager updated callback
  RolloutApplyFunction rollout_apply_function_;
  // Rollouts refresh check interval in ms
  int refresh_interval_ms_;
  // ServiceManagement service client instance
  std::unique_ptr<ServiceManagementFetch> service_management_fetch_;
};

}  // namespace api_manager
}  // namespace google
#endif  // API_MANAGER_CONFIG_MANAGER_H_
