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

#include "src/check_cache.h"
#include "gmock/gmock.h"
#include "gtest/gtest.h"
#include "utils/protobuf.h"
#include "utils/status_test_util.h"

using namespace std::chrono;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

time_point<system_clock> FakeTime(int t) {
  return time_point<system_clock>(milliseconds(t));
}

class CheckCacheTest : public ::testing::Test {
 public:
  void SetUp() {
    CheckOptions options(1 /*entries*/);
    options.cache_keys = {"string-key"};

    cache_ = std::unique_ptr<CheckCache>(new CheckCache(options));
    ASSERT_TRUE((bool)(cache_));

    attributes_.attributes["string-key"] =
        Attributes::StringValue("this-is-a-string-value");
  }

  void VerifyDisabledCache() {
    std::string signature;
    CheckResponse ok_response;
    ok_response.mutable_precondition()->set_valid_use_count(1000);
    // Just to calculate signature
    EXPECT_ERROR_CODE(Code::NOT_FOUND,
                      cache_->Check(attributes_, FakeTime(0), &signature));
    // set to the cache
    EXPECT_OK(cache_->CacheResponse(signature, ok_response, FakeTime(0)));

    // Still not_found, so cache is disabled.
    EXPECT_ERROR_CODE(Code::NOT_FOUND,
                      cache_->Check(attributes_, FakeTime(0), &signature));
  }

  Status Check(const Attributes& request, time_point<system_clock> time_now,
               std::string* signature) {
    return cache_->Check(request, time_now, signature);
  }
  Status CacheResponse(const std::string& signature,
                       const ::istio::mixer::v1::CheckResponse& response,
                       time_point<system_clock> time_now) {
    return cache_->CacheResponse(signature, response, time_now);
  }

  Attributes attributes_;
  std::unique_ptr<CheckCache> cache_;
};

TEST_F(CheckCacheTest, TestDisableCacheFromZeroCacheSize) {
  // 0 cache entries. cache is disabled
  CheckOptions options(0);
  options.cache_keys = {"string-key"};
  cache_ = std::unique_ptr<CheckCache>(new CheckCache(options));

  ASSERT_TRUE((bool)(cache_));
  VerifyDisabledCache();
}

TEST_F(CheckCacheTest, TestDisableCacheFromEmptyCacheKeys) {
  // empty cache keys. cache is disabled
  CheckOptions options(1000 /*entries*/);
  cache_ = std::unique_ptr<CheckCache>(new CheckCache(options));

  ASSERT_TRUE((bool)(cache_));
  VerifyDisabledCache();
}

TEST_F(CheckCacheTest, TestNeverExpired) {
  std::string signature;
  EXPECT_ERROR_CODE(Code::NOT_FOUND,
                    Check(attributes_, FakeTime(0), &signature));

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(10000);
  EXPECT_OK(CacheResponse(signature, ok_response, FakeTime(0)));
  for (int i = 0; i < 1000; ++i) {
    EXPECT_OK(Check(attributes_, FakeTime(i * 1000000), &signature));
  }
}

TEST_F(CheckCacheTest, TestExpiredByUseCount) {
  std::string signature;
  EXPECT_ERROR_CODE(Code::NOT_FOUND,
                    Check(attributes_, FakeTime(0), &signature));

  CheckResponse ok_response;
  // valid_use_count = 3
  ok_response.mutable_precondition()->set_valid_use_count(3);
  EXPECT_OK(CacheResponse(signature, ok_response, FakeTime(0)));

  // 3 requests are OK
  EXPECT_OK(Check(attributes_, FakeTime(1 * 1000000), &signature));
  EXPECT_OK(Check(attributes_, FakeTime(2 * 1000000), &signature));
  EXPECT_OK(Check(attributes_, FakeTime(3 * 1000000), &signature));

  // The 4th one should fail.
  EXPECT_ERROR_CODE(Code::NOT_FOUND,
                    Check(attributes_, FakeTime(4 * 1000000), &signature));
}

TEST_F(CheckCacheTest, TestExpiredByDuration) {
  std::string signature;
  EXPECT_ERROR_CODE(Code::NOT_FOUND,
                    Check(attributes_, FakeTime(0), &signature));

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  // expired in 10 milliseconds.
  *ok_response.mutable_precondition()->mutable_valid_duration() =
      CreateDuration(duration_cast<nanoseconds>(milliseconds(10)));
  EXPECT_OK(CacheResponse(signature, ok_response, FakeTime(0)));

  // OK, In 1 milliseconds.
  EXPECT_OK(Check(attributes_, FakeTime(1), &signature));

  // Not found in 11 milliseconds.
  EXPECT_ERROR_CODE(Code::NOT_FOUND,
                    Check(attributes_, FakeTime(11), &signature));
}

TEST_F(CheckCacheTest, TestCheckResult) {
  CheckCache::CheckResult result;
  cache_->Check(attributes_, &result);
  EXPECT_FALSE(result.IsCacheHit());

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  result.SetResponse(Status::OK, ok_response);
  EXPECT_OK(result.status());

  for (int i = 0; i < 100; ++i) {
    CheckCache::CheckResult result;
    cache_->Check(attributes_, &result);
    EXPECT_TRUE(result.IsCacheHit());
    EXPECT_OK(result.status());
  }
}

TEST_F(CheckCacheTest, TestInvalidResult) {
  CheckCache::CheckResult result;
  cache_->Check(attributes_, &result);
  EXPECT_FALSE(result.IsCacheHit());

  // Precondition result is not set
  CheckResponse ok_response;
  result.SetResponse(Status::OK, ok_response);
  EXPECT_ERROR_CODE(Code::INVALID_ARGUMENT, result.status());

  // Not found due to last invalid result.
  CheckCache::CheckResult result1;
  cache_->Check(attributes_, &result1);
  EXPECT_FALSE(result1.IsCacheHit());
}

TEST_F(CheckCacheTest, TestCachedSetResponse) {
  CheckCache::CheckResult result;
  cache_->Check(attributes_, &result);
  EXPECT_FALSE(result.IsCacheHit());

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  result.SetResponse(Status::OK, ok_response);
  EXPECT_OK(result.status());

  // Found in the cache
  CheckCache::CheckResult result1;
  cache_->Check(attributes_, &result1);
  EXPECT_TRUE(result1.IsCacheHit());
  EXPECT_OK(result1.status());

  // Set a negative response.
  ok_response.mutable_precondition()->mutable_status()->set_code(
      Code::UNAVAILABLE);
  result1.SetResponse(Status::OK, ok_response);
  EXPECT_ERROR_CODE(Code::UNAVAILABLE, result1.status());
}

}  // namespace mixer_client
}  // namespace istio
