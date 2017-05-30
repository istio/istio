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

namespace google {
namespace api_manager {

namespace {
// Default rollouts refresh interval in ms
const int kCheckNewRolloutInterval = 60000;

// static configs for error handling
static std::vector<std::pair<std::string, int>> kEmptyConfigs;
}  // namespace anonymous

ConfigManager::ConfigManager(
    std::shared_ptr<context::GlobalContext> global_context,
    RolloutApplyFunction rollout_apply_function)
    : global_context_(global_context),
      rollout_apply_function_(rollout_apply_function),
      refresh_interval_ms_(kCheckNewRolloutInterval) {
  if (global_context_->server_config()->has_service_management_config()) {
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

void ConfigManager::Init() {
  if (global_context_->service_name().empty() ||
      global_context_->config_id().empty()) {
    GlobalFetchGceMetadata(global_context_, [this](utils::Status status) {
      OnFetchMetadataDone(status);
    });
  } else {
    OnFetchMetadataDone(utils::Status::OK);
  }
}

void ConfigManager::OnFetchMetadataDone(utils::Status status) {
  if (!status.ok()) {
    // We should not get here
    global_context_->env()->LogError("Unexpected status: " + status.ToString());
    rollout_apply_function_(utils::Status(Code::ABORTED, status.ToString()),
                            kEmptyConfigs);
    return;
  }

  // Update service_name
  if (global_context_->service_name().empty()) {
    global_context_->set_service_name(
        global_context_->gce_metadata()->endpoints_service_name());
  }

  // Update config_id
  if (global_context_->config_id().empty()) {
    global_context_->config_id(
        global_context_->gce_metadata()->endpoints_service_config_id());
  }

  // Call ApiManager with status Code::ABORTED, ESP will stop moving forward
  if (global_context_->service_name().empty()) {
    std::string msg = "API service name is not specified";
    global_context_->env()->LogError(msg);
    rollout_apply_function_(utils::Status(Code::ABORTED, msg), kEmptyConfigs);
    return;
  }

  // TODO(jaebong) config_id should not be empty for the first version
  // This part will be removed after the rollouts feature added
  if (global_context_->config_id().empty()) {
    std::string msg = "API config_id is not specified";
    global_context_->env()->LogError(msg);
    rollout_apply_function_(utils::Status(Code::ABORTED, msg), kEmptyConfigs);
    return;
  }

  // Fetch service account token
  GlobalFetchServiceAccountToken(global_context_, [this](utils::Status status) {
    OnFetchAuthTokenDone(status);
  });
}

void ConfigManager::OnFetchAuthTokenDone(utils::Status status) {
  if (!status.ok()) {
    // We should not get here
    global_context_->env()->LogError("Unexpected status: " + status.ToString());
    rollout_apply_function_(utils::Status(Code::ABORTED, status.ToString()),
                            kEmptyConfigs);
    return;
  }

  // Fetch configs from the Inception
  // For now, config manager has only one config_id (100% rollout)
  std::shared_ptr<ConfigsFetchInfo> config_fetch_info(
      new ConfigsFetchInfo({{global_context_->config_id(), 100}}));

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
        // Failed to fetch all configs or rollouts are empty
        if (config_fetch_info->IsRolloutsEmpty() ||
            config_fetch_info->IsConfigsEmpty()) {
          // first time, call the ApiManager callback function with an error
          rollout_apply_function_(
              utils::Status(Code::ABORTED,
                            "Failed to download the service config"),
              kEmptyConfigs);
          return;
        }

        // Update ApiManager
        rollout_apply_function_(utils::Status::OK, config_fetch_info->configs);
      }
    });
  }
}

}  // namespace api_manager
}  // namespace google
