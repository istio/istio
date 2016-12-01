// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/auth/jwt_cache.h"

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
