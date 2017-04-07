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
#include "contrib/endpoints/src/api_manager/service_control/url.h"

namespace google {
namespace api_manager {
namespace service_control {

namespace {

// Service Control check and report URL paths:
//   /v1/services/{service}:check
//   /v1/services/{service}:report
const char v1_services_path[] = "/v1/services/";
const char check_verb[] = ":check";
const char quota_verb[] = ":allocateQuota";
const char report_verb[] = ":report";
const char http[] = "http://";
const char https[] = "https://";

// Finds service control server URL. Supports server config override
const std::string& GetServiceControlAddress(
    const ::google::api::Service* service,
    const proto::ServerConfig* server_config) {
  // Return the value from the server config override if present.
  if (server_config && server_config->has_service_control_config()) {
    const ::google::api_manager::proto::ServiceControlConfig& scc =
        server_config->service_control_config();
    if (!scc.url_override().empty()) {
      return scc.url_override();
    }
  }

  // Otherwise, use the default from service config.
  return service->control().environment();
}

}  // namespace

Url::Url(const ::google::api::Service* service,
         const proto::ServerConfig* server_config) {
  // Precompute check and report URLs
  if (service) {
    service_control_ = GetServiceControlAddress(service, server_config);
  }

  if (!service_control_.empty()) {
    if (service_control_.compare(0, sizeof(http) - 1, http) != 0 &&
        service_control_.compare(0, sizeof(https) - 1, https) != 0) {
      service_control_ = https + service_control_;  // https is default
    }

    std::string path = service_control_ + v1_services_path + service->name();
    check_url_ = path + check_verb;
    report_url_ = path + report_verb;
    quota_url_ = path + quota_verb;
  }
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
