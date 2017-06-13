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
#ifndef API_MANAGER_CONTEXT_GLOBAL_CONTEXT_H_
#define API_MANAGER_CONTEXT_GLOBAL_CONTEXT_H_

#include "contrib/endpoints/src/api_manager/auth/certs.h"
#include "contrib/endpoints/src/api_manager/auth/jwt_cache.h"
#include "contrib/endpoints/src/api_manager/auth/service_account_token.h"
#include "contrib/endpoints/src/api_manager/cloud_trace/cloud_trace.h"
#include "contrib/endpoints/src/api_manager/gce_metadata.h"
#include "contrib/endpoints/src/api_manager/proto/server_config.pb.h"

namespace google {
namespace api_manager {

namespace context {

// A global context shared across all services. It stores
// * env
// * server_config
// * service_account_token
// * certs and jwt_cache
// * metadata server and fetched data.
// * cloud trace object.
class GlobalContext {
 public:
  GlobalContext(std::unique_ptr<ApiManagerEnvInterface> env,
                const std::string &server_config);

  // the env interface.
  ApiManagerEnvInterface *env() { return env_.get(); }

  // the service account token store
  auth::ServiceAccountToken *service_account_token() {
    return &service_account_token_;
  }

  const std::string &metadata_server() const { return metadata_server_; }

  // fetched metadata.
  GceMetadata *gce_metadata() { return &gce_metadata_; }

  // cloud_trace
  cloud_trace::Aggregator *cloud_trace_aggregator() const {
    return cloud_trace_aggregator_.get();
  }

  std::shared_ptr<proto::ServerConfig> server_config() {
    return server_config_;
  }

  bool DisableLogStatus() {
    if (server_config() && server_config()->has_experimental()) {
      const auto &experimental = server_config()->experimental();
      return experimental.disable_log_status();
    }
    return false;
  }

  // report interval can be override by server_config.
  int64_t intermediate_report_interval() const {
    return intermediate_report_interval_;
  }

  // Check if auth is disabled from server_config.
  bool is_auth_force_disabled() const { return is_auth_force_disabled_; }

  // get producer project id from fetched metadata
  const std::string &project_id() const;

  const std::string &service_name() const { return service_name_; }
  void set_service_name(const std::string &name) { service_name_ = name; }

  const std::string &rollout_strategy() const { return rollout_strategy_; }
  void rollout_strategy(const std::string &rollout_strategy) {
    rollout_strategy_ = rollout_strategy;
  }

 private:
  // create cloud trace.
  std::unique_ptr<cloud_trace::Aggregator> CreateCloudTraceAggregator();

  std::unique_ptr<ApiManagerEnvInterface> env_;

  std::shared_ptr<proto::ServerConfig> server_config_;

  // service account tokens
  auth::ServiceAccountToken service_account_token_;

  // The service control object. When trace is force disabled, this will be a
  // nullptr.
  std::unique_ptr<cloud_trace::Aggregator> cloud_trace_aggregator_;

  // service name;
  std::string service_name_;
  // rollout strategy;
  std::string rollout_strategy_;

  // meta data server.
  std::string metadata_server_;
  // GCE metadata
  GceMetadata gce_metadata_;

  // Is auth force-disabled
  bool is_auth_force_disabled_;

  // The time interval for grpc intermediate report.
  int64_t intermediate_report_interval_;
};

}  // namespace context
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_CONTEXT_GLOBAL_CONTEXT_H_
