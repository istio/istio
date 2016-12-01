/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */
#ifndef API_MANAGER_AUTH_JWT_CACHE_H_
#define API_MANAGER_AUTH_JWT_CACHE_H_

#include <chrono>
#include <string>

#include "src/api_manager/auth.h"
#include "third_party/service-control-client-cxx/utils/simple_lru_cache_inl.h"

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
