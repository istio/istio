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
#ifndef API_MANAGER_SERVICE_MANAGEMENT_FETCH_H_
#define API_MANAGER_SERVICE_MANAGEMENT_FETCH_H_

#include <functional>
#include <string>

#include "contrib/endpoints/src/api_manager/context/global_context.h"
#include "google/api/servicemanagement/v1/servicemanager.pb.h"

using ::google::api::Service;
using ::google::api::servicemanagement::v1::ListServiceRolloutsResponse;
using ::google::api::servicemanagement::v1::Rollout_RolloutStatus;

namespace google {
namespace api_manager {

namespace {
// HTTP request callback
typedef std::function<void(const utils::Status&, std::string&&)>
    HttpCallbackFunction;
}

class ServiceManagementFetch {
 public:
  ServiceManagementFetch(
      std::shared_ptr<context::GlobalContext> global_context);
  virtual ~ServiceManagementFetch(){};

  // Fetches Service Rollouts
  void GetRollouts(HttpCallbackFunction on_done);

  // Fetches ServiceConfig from the ServiceManagement service
  void GetConfig(const std::string& config_id, HttpCallbackFunction callback);

 private:
  // Make a http GET request
  void Call(const std::string& url, HttpCallbackFunction on_done);
  // Generate Auth Token
  const std::string& GetAuthToken();

  // Global context
  std::shared_ptr<context::GlobalContext> global_context_;
  // ServiceManagement API host url. the default value is
  // https://servicemanagement.googleapis.com
  std::string host_;
};

}  // namespace api_manager
}  // namespace google
#endif  // API_MANAGER_SERVICE_MANAGEMENT_FETCH_H_
