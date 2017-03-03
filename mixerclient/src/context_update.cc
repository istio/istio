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
#include "src/context_update.h"

namespace istio {
namespace mixer_client {

void ContextUpdate::UpdateStart() {
  curr_set_.clear();
  for (const auto& it : map_) {
    curr_set_.insert(it.first);
  }
}

bool ContextUpdate::Update(int index, Attributes::Value value,
                           CompValueFunc cmp_func) {
  auto it = map_.find(index);
  bool same = false;
  if (it != map_.end()) {
    if (it->second == value) {
      same = true;
    } else {
      if (cmp_func) {
        cmp_func(it->second, value);
      }
    }
  }
  if (!same) {
    map_[index] = value;
  }
  curr_set_.erase(index);
  return same;
}

std::set<int> ContextUpdate::UpdateFinish() {
  for (const auto it : curr_set_) {
    map_.erase(it);
  }
  return curr_set_;
}

}  // namespace mixer_client
}  // namespace istio
