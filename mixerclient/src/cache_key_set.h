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

#ifndef MIXER_CLIENT_CACHE_KEY_SET_H_
#define MIXER_CLIENT_CACHE_KEY_SET_H_

#include <map>
#include <set>
#include <string>
#include <vector>

namespace istio {
namespace mixer_client {

// A class to store sub key set
class SubKeySet {
 public:
  // Check if the sub key is in the set.
  bool Found(const std::string& sub_key) const {
    if (sub_keys_.empty()) {
      return true;
    } else {
      return sub_keys_.find(sub_key) != sub_keys_.end();
    }
  }

 private:
  friend class CacheKeySet;
  std::set<std::string> sub_keys_;
};

// A class to handle cache key set
// A std::set will not work for string map sub keys.
// For a StringMap attruibute,
// If cache key is "attribute.key", all values will be used.
// If cache key is "attribute.key/map.key", only the map key is used.
// If both format exists, the whole map will be used.
class CacheKeySet {
 public:
  CacheKeySet(const std::vector<std::string>& cache_keys);

  // Return nullptr if the key is not in the set.
  const SubKeySet* Find(const std::string& key) const {
    const auto it = key_map_.find(key);
    return it == key_map_.end() ? nullptr : &it->second;
  }

  bool empty() const { return key_map_.empty(); }

 private:
  std::map<std::string, SubKeySet> key_map_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXER_CLIENT_CACHE_KEY_SET_H_
