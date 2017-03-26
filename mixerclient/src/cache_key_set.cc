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

#include "src/cache_key_set.h"

namespace istio {
namespace mixer_client {

CacheKeySet::CacheKeySet(const std::vector<std::string>& cache_keys) {
  for (const std::string& key : cache_keys) {
    size_t pos = key.find_first_of('/');
    if (pos == std::string::npos) {
      // Always override with an empty one.
      // It handles case where
      // "key/sub_key" comes first, then "key" come.
      // In this case, "key" wins.
      key_map_[key] = SubKeySet();
    } else {
      std::string split_key = key.substr(0, pos);
      std::string split_sub_key = key.substr(pos + 1);
      auto it = key_map_.find(split_key);
      if (it == key_map_.end()) {
        key_map_[split_key] = SubKeySet();
        key_map_[split_key].sub_keys_.insert(split_sub_key);
      } else {
        // If map to empty set, it was created by "key"
        // without sub_key, keep it as empty set.
        if (!it->second.sub_keys_.empty()) {
          it->second.sub_keys_.insert(split_sub_key);
        }
      }
    }
  }
}

}  // namespace mixer_client
}  // namespace istio
