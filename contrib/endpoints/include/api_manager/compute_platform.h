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
#ifndef API_MANAGER_COMPUTE_PLATFORM_H_
#define API_MANAGER_COMPUTE_PLATFORM_H_

namespace google {
namespace api_manager {

namespace compute_platform {

enum ComputePlatform { UNKNOWN = 0, GAE_FLEX = 1, GCE = 2, GKE = 3 };

inline const char *ToString(ComputePlatform p) {
  switch (p) {
    case GAE_FLEX:
      return "GAE Flex";
    case GCE:
      return "GCE";
    case GKE:
      return "GKE";
    case UNKNOWN:
    default:
      return "unknown";
  }
}

}  // namespace compute_platform

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_COMPUTE_PLATFORM_H_
