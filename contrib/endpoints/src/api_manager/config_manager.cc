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
#include "contrib/endpoints/src/api_manager/config_manager.h"
#include "contrib/endpoints/src/api_manager/fetch_metadata.h"
#include "contrib/endpoints/src/api_manager/utils/marshalling.h"

namespace google {
namespace api_manager {

namespace {
// Default rollouts refresh interval in ms
const int kCheckNewRolloutInterval = 60000;

const char kRolloutStrategyManaged[] = "managed";

// static configs for error handling
static std::vector<std::pair<std::string, int>> kEmptyConfigs;
}  // namespace anonymous

ConfigManager::ConfigManager(
    std::shared_ptr<context::GlobalContext> global_context,
    RolloutApplyFunction rollout_apply_function)
    : global_context_(global_context),
      rollout_apply_function_(rollout_apply_function),
      refresh_interval_ms_(kCheckNewRolloutInterval) {
  if (global_context_->server_config() &&
      global_context_->server_config()->has_service_management_config()) {
    // update refresh interval in ms
    if (global_context_->server_config()
            ->service_management_config()
            .refresh_interval_ms() > 0) {
      refresh_interval_ms_ = global_context_->server_config()
                                 ->service_management_config()
                                 .refresh_interval_ms();
    }
  }

  service_management_fetch_.reset(new ServiceManagementFetch(global_context));
}

ConfigManager::~ConfigManager() {
  if (rollouts_refresh_timer_) {
    rollouts_refresh_timer_->Stop();
  }
};

void ConfigManager::Init() {
  if (global_context_->rollout_strategy() == kRolloutStrategyManaged &&
      refresh_interval_ms_ > 0) {
    rollouts_refresh_timer_ = global_context_->env()->StartPeriodicTimer(
        std::chrono::milliseconds(refresh_interval_ms_),
        [this]() { OnRolloutsRefreshTimer(); });
  }
}

void ConfigManager::OnRolloutsRefreshTimer() {
  GlobalFetchServiceAccountToken(global_context_, [this](utils::Status status) {
    if (!status.ok()) {
      global_context_->env()->LogError("Unexpected status: " +
                                       status.ToString());
      return;
    }
    FetchRollouts();
  });
}

void ConfigManager::FetchRollouts() {
  service_management_fetch_->GetRollouts(
      [this](const utils::Status& status, std::string&& rollouts) {
        OnRolloutResponse(status, std::move(rollouts));
      });
}

void ConfigManager::OnRolloutResponse(const utils::Status& status,
                                      std::string&& rollouts) {
  if (!status.ok()) {
    global_context_->env()->LogError(
        std::string("Failed to download rollouts: ") + status.ToString() +
        ", Response body: " + rollouts);
    return;
  }

  ListServiceRolloutsResponse response;
  if (!utils::JsonToProto(rollouts, (::google::protobuf::Message*)&response)
           .ok()) {
    global_context_->env()->LogError(std::string("Invalid response: ") +
                                     status.ToString() + ", Response body: " +
                                     rollouts);
    return;
  }

  if (response.rollouts_size() == 0) {
    global_context_->env()->LogError("No active rollouts");
    return;
  }

  if (current_rollout_id_ == response.rollouts(0).rollout_id()) {
    return;
  }

  std::shared_ptr<ConfigsFetchInfo> config_fetch_info =
      std::make_shared<ConfigsFetchInfo>();

  config_fetch_info->rollout_id = response.rollouts(0).rollout_id();

  for (auto percentage :
       response.rollouts(0).traffic_percent_strategy().percentages()) {
    config_fetch_info->rollouts.push_back(
        {percentage.first, round(percentage.second)});
  }

  if (config_fetch_info->rollouts.size() == 0) {
    global_context_->env()->LogError("No active rollouts");
    return;
  }

  FetchConfigs(config_fetch_info);
}

// Fetch configs from rollouts. fetch_info has rollouts and fetched configs
void ConfigManager::FetchConfigs(
    std::shared_ptr<ConfigsFetchInfo> config_fetch_info) {
  for (auto rollout : config_fetch_info->rollouts) {
    std::string config_id = rollout.first;
    int percentage = rollout.second;
    service_management_fetch_->GetConfig(config_id, [this, config_id,
                                                     percentage,
                                                     config_fetch_info](
                                                        const utils::Status&
                                                            status,
                                                        std::string&& config) {

      if (status.ok()) {
        config_fetch_info->configs.push_back({std::move(config), percentage});
      } else {
        global_context_->env()->LogError(std::string(
            "Unable to download Service config for the config_id: " +
            config_id));
      }

      config_fetch_info->finished++;

      if (config_fetch_info->IsCompleted()) {
        if (config_fetch_info->IsRolloutsEmpty() ||
            config_fetch_info->IsConfigsEmpty() ||
            config_fetch_info->rollouts.size() !=
                config_fetch_info->configs.size()) {
          global_context_->env()->LogError(
              "Failed to download the service config");
          return;
        }

        // Update ApiManager
        rollout_apply_function_(utils::Status::OK,
                                std::move(config_fetch_info->configs));
        current_rollout_id_ = config_fetch_info->rollout_id;
      }
    });
  }
}

}  // namespace api_manager
}  // namespace google
