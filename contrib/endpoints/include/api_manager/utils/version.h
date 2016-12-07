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

#ifndef API_MANAGER_UTILS_VERSION_H_
#define API_MANAGER_UTILS_VERSION_H_

#include <string>

namespace google {
namespace api_manager {
namespace utils {

// The version is used by Endpoint cloud trace and service control
// when sending service agent.
class Version final {
 public:
  // Fetch singleton.
  static Version& instance();

  // Get the version.
  const std::string& get() const { return version_; }

  // Set the version.  Default is empty.
  void set(const std::string& v) { version_ = v; }

 private:
  // The version.
  std::string version_;

  // public construct not allowed
  Version() {}
};

}  // namespace utils
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_UTILS_VERSION_H_
