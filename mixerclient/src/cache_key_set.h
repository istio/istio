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

#include <memory>
#include <string>
#include <vector>

namespace istio {
namespace mixer_client {

class SubKeySet {
 public:
  virtual ~SubKeySet() {}

  // Check if subkey should be used.
  virtual bool Found(const std::string& sub_key) const = 0;
};

class CacheKeySet {
 public:
  virtual ~CacheKeySet() {}

  // Check if a key should be used, return nullptr if no.
  virtual const SubKeySet* Find(const std::string& key) const = 0;

  // Only the keys in the inclusive_keys are used.
  // A class to handle cache key set
  // A std::set will not work for string map sub keys.
  // For a StringMap attruibute,
  // If cache key is "attribute.key", all values will be used.
  // If cache key is "attribute.key/map.key", only the map key is used.
  // If both format exists, the whole map will be used.
  static std::unique_ptr<CacheKeySet> CreateInclusive(
      const std::vector<std::string>& inclusive_keys);

  // Only the keys NOT in the exclusive_keys are used.
  static std::unique_ptr<CacheKeySet> CreateExclusive(
      const std::vector<std::string>& exclusive_keys);

  // All keys are used.
  static std::unique_ptr<CacheKeySet> CreateAll();
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXER_CLIENT_CACHE_KEY_SET_H_
