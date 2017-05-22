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
#ifndef API_MANAGER_GCE_METADATA_H_
#define API_MANAGER_GCE_METADATA_H_

#include "contrib/endpoints/include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// An environment information extracted from the Google Compute Engine metadata
// server.
class GceMetadata {
 public:
  enum FetchState {
    NONE = 0,
    // Fetching,
    FETCHING,
    // Fetch failed
    FAILED,
    // Data is go
    FETCHED,
  };
  GceMetadata() : state_(NONE) {}

  FetchState state() const { return state_; }
  void set_state(FetchState state) { state_ = state; }
  bool has_valid_data() const { return state_ == FETCHED; }

  utils::Status ParseFromJson(std::string* json);

  const std::string& project_id() const { return project_id_; }
  const std::string& zone() const { return zone_; }
  const std::string& gae_server_software() const {
    return gae_server_software_;
  }
  const std::string& kube_env() const { return kube_env_; }
  const std::string& endpoints_service_name() const {
    return endpoints_service_name_;
  }
  const std::string& endpoints_service_config_id() const {
    return endpoints_service_config_id_;
  }

 private:
  FetchState state_;
  std::string project_id_;
  std::string zone_;
  std::string gae_server_software_;
  std::string kube_env_;
  std::string endpoints_service_name_;
  std::string endpoints_service_config_id_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_GCE_METADATA_H_
