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
#ifndef API_MANAGER_CONTEXT_SERVICE_CONTEXT_H_
#define API_MANAGER_CONTEXT_SERVICE_CONTEXT_H_

#include "include/api_manager/method.h"
#include "src/api_manager/auth/certs.h"
#include "src/api_manager/auth/jwt_cache.h"
#include "src/api_manager/auth/service_account_token.h"
#include "src/api_manager/cloud_trace/cloud_trace.h"
#include "src/api_manager/config.h"
#include "src/api_manager/gce_metadata.h"
#include "src/api_manager/service_control/interface.h"

namespace google {
namespace api_manager {

namespace context {

// Shared context across request for every service
// Each RequestContext will hold a refcount to this object.
class ServiceContext {
 public:
  ServiceContext(std::unique_ptr<ApiManagerEnvInterface> env,
                 std::unique_ptr<Config> config);

  bool Enabled() const { return RequireAuth() || service_control_; }

  const std::string &service_name() const { return config_->service_name(); }

  const ::google::api::Service &service() const { return config_->service(); }

  void SetMetadataServer(const std::string &server) {
    metadata_server_ = server;
  }

  auth::ServiceAccountToken *service_account_token() {
    return &service_account_token_;
  }

  ApiManagerEnvInterface *env() { return env_.get(); }
  Config *config() { return config_.get(); }

  MethodCallInfo GetMethodCallInfo(const std::string &http_method,
                                   const std::string &url,
                                   const std::string &query_params) const;

  service_control::Interface *service_control() const {
    return service_control_.get();
  }

  bool RequireAuth() const {
    return !is_auth_force_disabled_ && config_->HasAuth();
  }

  auth::Certs &certs() { return certs_; }
  auth::JwtCache &jwt_cache() { return jwt_cache_; }

  bool GetJwksUri(const std::string &issuer, std::string *url) {
    return config_->GetJwksUri(issuer, url);
  }

  void SetJwksUri(const std::string &issuer, const std::string &jwks_uri,
                  bool openid_valid) {
    config_->SetJwksUri(issuer, jwks_uri, openid_valid);
  }

  const std::string &metadata_server() const { return metadata_server_; }
  GceMetadata *gce_metadata() { return &gce_metadata_; }
  const std::string &project_id() const;
  cloud_trace::Aggregator *cloud_trace_aggregator() const {
    return cloud_trace_aggregator_.get();
  }

  bool DisableLogStatus() {
    if (config_->server_config() &&
        config_->server_config()->has_experimental()) {
      const auto &experimental = config_->server_config()->experimental();
      return experimental.disable_log_status();
    }
    return false;
  }

  int64_t intermediate_report_interval() const {
    return intermediate_report_interval_;
  }

 private:
  std::unique_ptr<service_control::Interface> CreateInterface();

  std::unique_ptr<cloud_trace::Aggregator> CreateCloudTraceAggregator();

  std::unique_ptr<ApiManagerEnvInterface> env_;
  std::unique_ptr<Config> config_;

  auth::Certs certs_;
  auth::JwtCache jwt_cache_;

  // service account tokens
  auth::ServiceAccountToken service_account_token_;

  // The service control object.
  std::unique_ptr<service_control::Interface> service_control_;

  // The service control object. When trace is force disabled, this will be a
  // nullptr.
  std::unique_ptr<cloud_trace::Aggregator> cloud_trace_aggregator_;

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

#endif  // API_MANAGER_CONTEXT_SERVICE_CONTEXT_H_
