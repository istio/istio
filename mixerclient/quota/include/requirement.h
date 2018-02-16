/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#ifndef QUOTA_REQUIREMENT_H_
#define QUOTA_REQUIREMENT_H_

#include <string>

namespace istio {
namespace quota {

// A struct to represent one quota requirement.
struct Requirement {
  // The quota name.
  std::string quota;
  // The amount to charge
  int64_t charge;
};

}  // namespace quota
}  // namespace istio

#endif  // QUOTA_REQUIREMENT_H_
