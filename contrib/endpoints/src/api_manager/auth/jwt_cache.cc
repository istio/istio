// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/api_manager/auth/jwt_cache.h"

using ::google::service_control_client::SimpleLRUCache;
using std::chrono::system_clock;

namespace google {
namespace api_manager {
namespace auth {

namespace {
// The maximum lifetime of a cache entry. Unit: seconds.
// TODO: This value should be configurable via server config.
const int kJwtCacheTimeout = 300;
// The number of entries in JWT cache.
// TODO: This value should be configurable via server config.
const int kJwtCacheSize = 100;
}  // namespace

JwtCache::JwtCache() : SimpleLRUCache<std::string, JwtValue>(kJwtCacheSize) {}

JwtCache::~JwtCache() { Clear(); }

void JwtCache::Insert(const std::string& jwt, const UserInfo& user_info,
                      const system_clock::time_point& token_exp,
                      const system_clock::time_point& now) {
  JwtValue* newval = new JwtValue();
  newval->user_info = user_info;
  newval->exp =
      std::min(token_exp, now + std::chrono::seconds(kJwtCacheTimeout));
  SimpleLRUCache::Insert(jwt, newval, 1);
}

}  // namespace auth
}  // namespace api_manager
}  // namespace google
