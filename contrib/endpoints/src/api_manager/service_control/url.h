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
#ifndef API_MANAGER_SERVICE_CONTROL_URL_H_
#define API_MANAGER_SERVICE_CONTROL_URL_H_

#include "contrib/endpoints/src/api_manager/proto/server_config.pb.h"
#include "google/api/service.pb.h"

namespace google {
namespace api_manager {
namespace service_control {

// This class implements service control url related logic.
class Url {
 public:
  Url(const ::google::api::Service* service,
      const ::google::api_manager::proto::ServerConfig* server_config);

  // Pre-computed url for service control.
  const std::string& service_control() const { return service_control_; }
  const std::string& check_url() const { return check_url_; }
  const std::string& quota_url() const { return quota_url_; }
  const std::string& report_url() const { return report_url_; }

 private:
  // Pre-computed url for service control methods.
  std::string service_control_;
  std::string check_url_;
  std::string quota_url_;
  std::string report_url_;
};

}  // namespace service_control
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_SERVICE_CONTROL_URL_H_
