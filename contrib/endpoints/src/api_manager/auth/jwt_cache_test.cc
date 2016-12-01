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
#include "src/api_manager/auth/jwt_cache.h"
#include <memory>
#include "gtest/gtest.h"

using std::chrono::system_clock;

namespace google {
namespace api_manager {
namespace auth {

namespace {

const char kId[] = "user1";
const char kEmail[] = "user1@gmail.com";
const char kConsumer[] = "consumer1";
const char kIssuer[] = "iss1";
const char kJwt[] =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1M"
    "jNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20"
    "iLCJzdWIiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZ"
    "GV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJodHRwOi8vbXlzZXJ2aWNlLmN"
    "vbS9teWFwaSJ9.gq_4ucjddQDjYK5FJr_kXmMo2fgSEB6Js1zopcQLVpCKFDNb-TQ97go0wuk5"
    "_vlSp_8I2ImrcdwYbAKqYCzcdyBXkAYoHCGgmY-v6MwZFUvrIaDzR_M3rmY8sQ8cdN3MN6ZRbB"
    "6opHwDP1lUEx4bZn_ZBjJMPgqbIqGmhoT1UpfPF6P1eI7sXYru-4KVna0STOynLl3d7JYb7E-8"
    "ifcjUJLhat8JR4zR8i4-zWjn6d6j_NI7ZvMROnao77D9YyhXv56zfsXRatKzzYtxPlQMz4AjP-"
    "bUHfbHmhiIOOAeEKFuIVUAwM17j54M6VQ5jnAabY5O-ermLfwPiXvNt2L2SA==";
const int kJwtCacheTimeout = 300;

class TestJwtCache : public ::testing::Test {
 public:
  virtual void SetUp() { cache_.reset(new JwtCache()); }

  std::unique_ptr<JwtCache> cache_;
};

// Test the Insert function in JwtCache class.
void InsertAndLookupImpl(JwtCache *cache, bool token_exp_earlier) {
  ASSERT_EQ(nullptr, cache->Lookup(kJwt));

  UserInfo user_info;
  user_info.id = kId;
  user_info.email = kEmail;
  user_info.consumer_id = kConsumer;
  user_info.issuer = kIssuer;
  user_info.audiences.insert("aud1");
  user_info.audiences.insert("aud2");
  system_clock::time_point now = system_clock::now();

  system_clock::time_point token_exp;
  if (token_exp_earlier) {
    token_exp = now + std::chrono::seconds(kJwtCacheTimeout - 1);
  } else {
    token_exp = now + std::chrono::seconds(kJwtCacheTimeout + 1);
  }
  cache->Insert(kJwt, user_info, token_exp, now);
  JwtValue *val = cache->Lookup(kJwt);
  ASSERT_NE(nullptr, val);
  ASSERT_EQ(val->user_info.id, kId);
  ASSERT_EQ(val->user_info.email, kEmail);
  ASSERT_EQ(val->user_info.consumer_id, kConsumer);
  ASSERT_EQ(val->user_info.issuer, kIssuer);
  ASSERT_EQ(val->user_info.AudiencesAsString(), "aud1,aud2");
  if (token_exp_earlier) {
    ASSERT_EQ(val->exp, token_exp);
  } else {
    ASSERT_EQ(val->exp, now + std::chrono::seconds(kJwtCacheTimeout));
  }

  cache->Release(kJwt, val);
  cache->Remove(kJwt);
  ASSERT_EQ(nullptr, cache->Lookup(kJwt));
}

TEST_F(TestJwtCache, InsertAndLookUp) {
  // case 1: token expire sooner than 5 minutes.
  InsertAndLookupImpl(cache_.get(), true);

  // case 2: token lifetime is 5 minutes.
  InsertAndLookupImpl(cache_.get(), false);
}

}  // namespace

}  // namespace auth
}  // namespace api_manager
}  // namespace google
