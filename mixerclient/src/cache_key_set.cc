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

#include "cache_key_set.h"
#include <map>
#include <set>

namespace istio {
namespace mixer_client {
namespace {

// A class to store sub key set
class SubKeySetImpl : public SubKeySet {
 public:
  // Check if the sub key is in the set.
  bool Found(const std::string& sub_key) const override {
    if (sub_keys_.empty()) {
      return true;
    } else {
      return sub_keys_.find(sub_key) != sub_keys_.end();
    }
  }

 private:
  friend class InclusiveCacheKeySet;
  std::set<std::string> sub_keys_;
};

class InclusiveCacheKeySet : public CacheKeySet {
 public:
  InclusiveCacheKeySet(const std::vector<std::string>& cache_keys) {
    for (const std::string& key : cache_keys) {
      size_t pos = key.find_first_of('/');
      if (pos == std::string::npos) {
        // Always override with an empty one.
        // It handles case where
        // "key/sub_key" comes first, then "key" come.
        // In this case, "key" wins.
        key_map_[key] = SubKeySetImpl();
      } else {
        std::string split_key = key.substr(0, pos);
        std::string split_sub_key = key.substr(pos + 1);
        auto it = key_map_.find(split_key);
        if (it == key_map_.end()) {
          key_map_[split_key] = SubKeySetImpl();
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

  // Return nullptr if the key is not in the set.
  const SubKeySet* Find(const std::string& key) const override {
    const auto it = key_map_.find(key);
    return it == key_map_.end() ? nullptr : &it->second;
  }

 private:
  std::map<std::string, SubKeySetImpl> key_map_;
};

// Exclusive key set
class ExclusiveCacheKeySet : public CacheKeySet {
 public:
  ExclusiveCacheKeySet(const std::vector<std::string>& exclusive_keys) {
    keys_.insert(exclusive_keys.begin(), exclusive_keys.end());
  }

  const SubKeySet* Find(const std::string& key) const override {
    const auto it = keys_.find(key);
    return it == keys_.end() ? &subkey_ : nullptr;
  }

 private:
  SubKeySetImpl subkey_;
  std::set<std::string> keys_;
};

// all key set
class AllCacheKeySet : public CacheKeySet {
 public:
  const SubKeySet* Find(const std::string& key) const override {
    return &subkey_;
  }

 private:
  SubKeySetImpl subkey_;
};

}  // namespace

std::unique_ptr<CacheKeySet> CacheKeySet::CreateInclusive(
    const std::vector<std::string>& inclusive_keys) {
  return std::unique_ptr<CacheKeySet>(new InclusiveCacheKeySet(inclusive_keys));
}

std::unique_ptr<CacheKeySet> CacheKeySet::CreateExclusive(
    const std::vector<std::string>& exclusive_keys) {
  return std::unique_ptr<CacheKeySet>(new ExclusiveCacheKeySet(exclusive_keys));
}

std::unique_ptr<CacheKeySet> CacheKeySet::CreateAll() {
  return std::unique_ptr<CacheKeySet>(new AllCacheKeySet());
}

}  // namespace mixer_client
}  // namespace istio
