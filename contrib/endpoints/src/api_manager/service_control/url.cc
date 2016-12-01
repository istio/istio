// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/service_control/url.h"

namespace google {
namespace api_manager {
namespace service_control {

namespace {

// Service Control check and report URL paths:
//   /v1/services/{service}:check
//   /v1/services/{service}:report
const char v1_services_path[] = "/v1/services/";
const char check_verb[] = ":check";
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
  }
}

}  // namespace service_control
}  // namespace api_manager
}  // namespace google
