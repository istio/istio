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
#include "contrib/endpoints/src/api_manager/api_manager_impl.h"
#include "contrib/endpoints/src/api_manager/check_workflow.h"
#include "contrib/endpoints/src/api_manager/request_handler.h"

#include <fstream>
#include <sstream>

namespace google {
namespace api_manager {

namespace {

const std::string kConfigRolloutManaged("managed");

}  // namespace anonymous

ApiManagerImpl::ApiManagerImpl(std::unique_ptr<ApiManagerEnvInterface> env,
                               const std::string &server_config)
    : global_context_(
          new context::GlobalContext(std::move(env), server_config)) {
  check_workflow_ = std::unique_ptr<CheckWorkflow>(new CheckWorkflow);
  check_workflow_->RegisterAll();
}

utils::Status ApiManagerImpl::LoadServiceRollouts() {
  if (!global_context_->server_config()) {
    std::string err_msg = "Invalid server config";
    global_context_->env()->LogError(err_msg);
    return utils::Status(Code::ABORTED, err_msg);
  }

  utils::Status config_loading_status(Code::UNAVAILABLE, "");

  if (global_context_->server_config()->has_service_config_rollout() &&
      global_context_->server_config()
              ->service_config_rollout()
              .traffic_percentages_size() > 0) {
    std::vector<std::pair<std::string, int>> list;
    for (auto item : global_context_->server_config()
                         ->service_config_rollout()
                         .traffic_percentages()) {
      std::ifstream config_file(item.first);
      if (config_file.is_open()) {
        std::stringstream content;
        content << config_file.rdbuf();
        config_file.close();

        list.push_back({content.str(), round(item.second)});
      } else {
        std::string err_msg =
            std::string("Failed to open an api service configuration file: ") +
            item.first;
        global_context_->env()->LogError(err_msg);
        config_loading_status = utils::Status(Code::NOT_FOUND, err_msg);
        break;
      }
    }

    if (config_loading_status.code() == Code::UNAVAILABLE && list.size() > 0) {
      config_loading_status = AddAndDeployConfigs(std::move(list), false);
    } else {
      service_context_map_.clear();
      config_loading_status =
          utils::Status(Code::ABORTED, "Invalid service config");
    }
  } else {
    std::string err_msg = "Service config was not specified";
    global_context_->env()->LogError(err_msg);
    config_loading_status = utils::Status(Code::ABORTED, err_msg);
  }

  return config_loading_status;
}

utils::Status ApiManagerImpl::AddAndDeployConfigs(
    std::vector<std::pair<std::string, int>> &&configs, bool initialize) {
  std::vector<std::pair<std::string, int>> list;
  for (auto item : configs) {
    std::string config_id;
    if (AddConfig(item.first, initialize, &config_id).ok()) {
      list.push_back({config_id, round(item.second)});
    } else {
      return utils::Status(Code::ABORTED, "Invalid service config");
    }
  }

  if (list.size() == 0) {
    std::string msg = "Invalid service config";
    global_context_->env()->LogError(msg);
    return utils::Status(Code::ABORTED, msg);
  }

  DeployConfigs(std::move(list));

  global_context_->env()->LogInfo("New rollout was deployed");

  return utils::Status::OK;
}

utils::Status ApiManagerImpl::AddConfig(const std::string &service_config,
                                        bool initialize,
                                        std::string *config_id) {
  std::unique_ptr<Config> config =
      Config::Create(global_context_->env(), service_config);
  if (config == nullptr) {
    return utils::Status(Code::INVALID_ARGUMENT, "Invalid service config");
  }

  std::string service_name = config->service().name();
  if (global_context_->service_name().empty()) {
    global_context_->set_service_name(service_name);
  } else {
    if (service_name != global_context_->service_name()) {
      auto err_msg = std::string("Mismatched service name; existing: ") +
                     global_context_->service_name() + ", new: " + service_name;
      global_context_->env()->LogError(err_msg);
      return utils::Status(Code::INVALID_ARGUMENT, err_msg);
    }
  }

  *config_id = config->service().id();

  auto context_service = std::make_shared<context::ServiceContext>(
      global_context_, std::move(config));
  if (initialize == true && context_service->service_control()) {
    context_service->service_control()->Init();
  }
  service_context_map_[*config_id] = context_service;

  return utils::Status::OK;
}

// Deploy these configs according to the traffic percentage.
void ApiManagerImpl::DeployConfigs(
    std::vector<std::pair<std::string, int>> &&list) {
  service_selector_.reset(new WeightedSelector(std::move(list)));
}

utils::Status ApiManagerImpl::Init() {
  if (global_context_->cloud_trace_aggregator()) {
    global_context_->cloud_trace_aggregator()->Init();
  }

  if (service_context_map_.empty()) {
    return utils::Status(Code::UNAVAILABLE,
                         "Service config loading was failed");
  }

  for (auto it : service_context_map_) {
    if (it.second->service_control()) {
      it.second->service_control()->Init();
    }
  }

  if (global_context_->rollout_strategy() == kConfigRolloutManaged) {
    config_manager_.reset(new ConfigManager(
        global_context_,
        [this](const utils::Status &status,
               std::vector<std::pair<std::string, int>> &&configs) {
          if (status.ok()) {
            AddAndDeployConfigs(std::move(configs), true);
          }
        }));

    if (global_context_->server_config()->has_service_config_rollout()) {
      config_manager_->set_current_rollout_id(global_context_->server_config()
                                                  ->service_config_rollout()
                                                  .rollout_id());
    }

    config_manager_->Init();
  }

  return utils::Status::OK;
}

utils::Status ApiManagerImpl::Close() {
  if (global_context_->cloud_trace_aggregator()) {
    global_context_->cloud_trace_aggregator()->SendAndClearTraces();
  }

  for (auto it : service_context_map_) {
    if (it.second->service_control()) {
      it.second->service_control()->Close();
    }
  }
  return utils::Status::OK;
}

bool ApiManagerImpl::Enabled() const {
  for (const auto &it : service_context_map_) {
    if (it.second->Enabled()) {
      return true;
    }
  }
  return false;
}

const std::string &ApiManagerImpl::service_name() const {
  return global_context_->service_name();
}

const ::google::api::Service &ApiManagerImpl::service(
    const std::string &config_id) const {
  const auto &it = service_context_map_.find(config_id);
  if (it != service_context_map_.end()) {
    return it->second->service();
  }
  static ::google::api::Service empty;
  return empty;
}

std::shared_ptr<context::ServiceContext> ApiManagerImpl::SelectService() {
  if (service_context_map_.empty()) {
    return nullptr;
  }
  const auto &it = service_context_map_.find(service_selector_->Select());
  if (it != service_context_map_.end()) {
    return it->second;
  }
  return nullptr;
}

utils::Status ApiManagerImpl::GetStatistics(
    ApiManagerStatistics *statistics) const {
  memset(&statistics->service_control_statistics, 0,
         sizeof(service_control::Statistics));
  for (const auto &it : service_context_map_) {
    if (it.second->service_control()) {
      service_control::Statistics stat;
      auto status = it.second->service_control()->GetStatistics(&stat);
      if (status.ok()) {
        statistics->service_control_statistics.Merge(stat);
      }
    }
  }
  return utils::Status::OK;
}

std::unique_ptr<RequestHandlerInterface> ApiManagerImpl::CreateRequestHandler(
    std::unique_ptr<Request> request_data) {
  return std::unique_ptr<RequestHandlerInterface>(new RequestHandler(
      check_workflow_, SelectService(), std::move(request_data)));
}

std::shared_ptr<ApiManager> ApiManagerFactory::CreateApiManager(
    std::unique_ptr<ApiManagerEnvInterface> env,
    const std::string &server_config) {
  return std::shared_ptr<ApiManager>(
      new ApiManagerImpl(std::move(env), server_config));
}

}  // namespace api_manager
}  // namespace google
