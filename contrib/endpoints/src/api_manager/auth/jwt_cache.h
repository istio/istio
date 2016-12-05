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
#ifndef API_MANAGER_AUTH_JWT_CACHE_H_
#define API_MANAGER_AUTH_JWT_CACHE_H_

#include <chrono>
#include <string>

#include "contrib/endpoints/src/api_manager/auth.h"
#include "utils/simple_lru_cache_inl.h"

namespace google {
namespace api_manager {
namespace auth {

// The value of a JwtCache entry.
struct JwtValue {
  // User info extracted from the JWT.
  UserInfo user_info;

  // Expiration time of the cache entry. This is the minimum of "exp" field in
  // the JWT and [the time this cache entry is added + kJwtCacheTimeout].
  std::chrono::system_clock::time_point exp;
};

// A local cache that resides in ESP. The key of the cache is a JWT,
// and the value is of type JwtValue.
class JwtCache
    : public google::service_control_client::SimpleLRUCache<std::string,
                                                            JwtValue> {
 public:
  JwtCache();
  ~JwtCache();

  void Insert(const std::string& jwt, const UserInfo& user_info,
              const std::chrono::system_clock::time_point& token_exp,
              const std::chrono::system_clock::time_point& now);
};

}  // namespace auth
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_AUTH_JWT_CACHE_H_
