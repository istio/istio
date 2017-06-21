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
//  - Code::UNAVAILABLE Not initialized yet. The default value.
//  - Code::OK          Successfully initialized
//  - Code::ABORTED     Initialization was failed
// configs - pairs of ServiceConfig in text and rollout percentage
typedef std::function<void(const utils::Status& status,
                           std::vector<std::pair<std::string, int>>&& configs)>
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
  // rollout id
  std::string rollout_id;
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
  // the periodic timer task initialize by Init() invokes the
  // rollout_apply_function when it successfully downloads the latest successful
  // rollout
  ConfigManager(std::shared_ptr<context::GlobalContext> global_context,
                RolloutApplyFunction rollout_apply_function);
  virtual ~ConfigManager();

 public:
  // Initialize the periodic timer task
  void Init();

  // Getter and setter of current_rollout_id_
  const std::string current_rollout_id() { return current_rollout_id_; }
  void set_current_rollout_id(const std::string rollout_id) {
    current_rollout_id_ = rollout_id;
  }

 private:
  // Fetch the latest rollouts
  void FetchRollouts();
  // Fetch ServiceConfig details from the latest successful rollouts
  // https://goo.gl/I2nD4M
  void FetchConfigs(std::shared_ptr<ConfigsFetchInfo> config_fetch_info);
  // Period timer task
  void OnRolloutsRefreshTimer();
  // Rollout response handler
  void OnRolloutResponse(const utils::Status& status, std::string&& rollouts);

  // Global context provided by ApiManager
  std::shared_ptr<context::GlobalContext> global_context_;
  // ApiManager updated callback
  RolloutApplyFunction rollout_apply_function_;
  // Rollouts refresh check interval in ms
  int refresh_interval_ms_;
  // ServiceManagement service client instance
  std::unique_ptr<ServiceManagementFetch> service_management_fetch_;
  // Periodic timer task to refresh rollouts
  std::unique_ptr<PeriodicTimer> rollouts_refresh_timer_;
  // Previous rollouts id
  std::string current_rollout_id_;
};

}  // namespace api_manager
}  // namespace google
#endif  // API_MANAGER_CONFIG_MANAGER_H_
