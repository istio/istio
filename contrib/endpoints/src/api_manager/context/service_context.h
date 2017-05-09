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

#include "contrib/endpoints/include/api_manager/method.h"
#include "contrib/endpoints/src/api_manager/config.h"
#include "contrib/endpoints/src/api_manager/context/global_context.h"
#include "contrib/endpoints/src/api_manager/service_control/interface.h"

namespace google {
namespace api_manager {

namespace context {

// Shared context across request for every service
// Each RequestContext will hold a refcount to this object.
class ServiceContext {
 public:
  ServiceContext(std::shared_ptr<GlobalContext> global_context,
                 std::unique_ptr<Config> config);
  // For unit-test only.  It will create a global context
  ServiceContext(std::unique_ptr<ApiManagerEnvInterface> env,
                 const std::string &server_config,
                 std::unique_ptr<Config> config);

  bool Enabled() const { return RequireAuth() || service_control_; }

  const std::string &service_name() const { return config_->service_name(); }

  const ::google::api::Service &service() const { return config_->service(); }

  Config *config() { return config_.get(); }

  auth::ServiceAccountToken *service_account_token() {
    return global_context_->service_account_token();
  }

  ApiManagerEnvInterface *env() { return global_context_->env(); }

  MethodCallInfo GetMethodCallInfo(const std::string &http_method,
                                   const std::string &url,
                                   const std::string &query_params) const;

  service_control::Interface *service_control() const {
    return service_control_.get();
  }

  bool RequireAuth() const {
    return !global_context_->is_auth_force_disabled() && config_->HasAuth();
  }

  bool IsRulesCheckEnabled() const {
    return RequireAuth() && service().apis_size() > 0 &&
           !config_->GetFirebaseServer().empty();
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

  const std::string &metadata_server() const {
    return global_context_->metadata_server();
  }
  GceMetadata *gce_metadata() { return global_context_->gce_metadata(); }
  const std::string &project_id() const;
  cloud_trace::Aggregator *cloud_trace_aggregator() const {
    return global_context_->cloud_trace_aggregator();
  }

  int64_t intermediate_report_interval() const {
    return global_context_->intermediate_report_interval();
  }

  std::shared_ptr<GlobalContext> global_context() { return global_context_; }

 private:
  // Create service control.
  std::unique_ptr<service_control::Interface> CreateInterface();

  // The shared global context object.
  std::shared_ptr<GlobalContext> global_context_;
  // The service config object.
  std::unique_ptr<Config> config_;

  auth::Certs certs_;
  auth::JwtCache jwt_cache_;

  // The service control object.
  std::unique_ptr<service_control::Interface> service_control_;
};

}  // namespace context
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_CONTEXT_SERVICE_CONTEXT_H_
