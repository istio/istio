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
using ::istio::mixer::v1::ReferencedAttributes;
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
    CheckOptions options;
    cache_ = std::unique_ptr<CheckCache>(new CheckCache(options));
    ASSERT_TRUE((bool)(cache_));

    attributes_.attributes["target.service"] =
        Attributes::StringValue("this-is-a-string-value");
  }

  void VerifyDisabledCache() {
    CheckResponse ok_response;
    ok_response.mutable_precondition()->set_valid_use_count(1000);
    // Just to calculate signature
    EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes_, FakeTime(0)));
    // set to the cache
    EXPECT_OK(cache_->CacheResponse(attributes_, ok_response, FakeTime(0)));

    // Still not_found, so cache is disabled.
    EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes_, FakeTime(0)));
  }

  Status Check(const Attributes& request, time_point<system_clock> time_now) {
    return cache_->Check(request, time_now);
  }
  Status CacheResponse(const Attributes& attributes,
                       const ::istio::mixer::v1::CheckResponse& response,
                       time_point<system_clock> time_now) {
    return cache_->CacheResponse(attributes, response, time_now);
  }

  Attributes attributes_;
  std::unique_ptr<CheckCache> cache_;
};

TEST_F(CheckCacheTest, TestDisableCacheFromZeroCacheSize) {
  // 0 cache entries. cache is disabled
  CheckOptions options(0);
  cache_ = std::unique_ptr<CheckCache>(new CheckCache(options));

  ASSERT_TRUE((bool)(cache_));
  VerifyDisabledCache();
}

TEST_F(CheckCacheTest, TestNeverExpired) {
  EXPECT_ERROR_CODE(Code::NOT_FOUND, Check(attributes_, FakeTime(0)));

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(10000);
  EXPECT_OK(CacheResponse(attributes_, ok_response, FakeTime(0)));
  for (int i = 0; i < 1000; ++i) {
    EXPECT_OK(Check(attributes_, FakeTime(i * 1000000)));
  }
}

TEST_F(CheckCacheTest, TestExpiredByUseCount) {
  EXPECT_ERROR_CODE(Code::NOT_FOUND, Check(attributes_, FakeTime(0)));

  CheckResponse ok_response;
  // valid_use_count = 3
  ok_response.mutable_precondition()->set_valid_use_count(3);
  EXPECT_OK(CacheResponse(attributes_, ok_response, FakeTime(0)));

  // 3 requests are OK
  EXPECT_OK(Check(attributes_, FakeTime(1 * 1000000)));
  EXPECT_OK(Check(attributes_, FakeTime(2 * 1000000)));
  EXPECT_OK(Check(attributes_, FakeTime(3 * 1000000)));

  // The 4th one should fail.
  EXPECT_ERROR_CODE(Code::NOT_FOUND, Check(attributes_, FakeTime(4 * 1000000)));
}

TEST_F(CheckCacheTest, TestExpiredByDuration) {
  EXPECT_ERROR_CODE(Code::NOT_FOUND, Check(attributes_, FakeTime(0)));

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  // expired in 10 milliseconds.
  *ok_response.mutable_precondition()->mutable_valid_duration() =
      CreateDuration(duration_cast<nanoseconds>(milliseconds(10)));
  EXPECT_OK(CacheResponse(attributes_, ok_response, FakeTime(0)));

  // OK, In 1 milliseconds.
  EXPECT_OK(Check(attributes_, FakeTime(1)));

  // Not found in 11 milliseconds.
  EXPECT_ERROR_CODE(Code::NOT_FOUND, Check(attributes_, FakeTime(11)));
}

TEST_F(CheckCacheTest, TestCheckResult) {
  CheckCache::CheckResult result;
  cache_->Check(attributes_, &result);
  EXPECT_FALSE(result.IsCacheHit());

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  result.SetResponse(Status::OK, attributes_, ok_response);
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
  result.SetResponse(Status::OK, attributes_, ok_response);
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
  result.SetResponse(Status::OK, attributes_, ok_response);
  EXPECT_OK(result.status());

  // Found in the cache
  CheckCache::CheckResult result1;
  cache_->Check(attributes_, &result1);
  EXPECT_TRUE(result1.IsCacheHit());
  EXPECT_OK(result1.status());

  // Set a negative response.
  ok_response.mutable_precondition()->mutable_status()->set_code(
      Code::UNAVAILABLE);
  result1.SetResponse(Status::OK, attributes_, ok_response);
  EXPECT_ERROR_CODE(Code::UNAVAILABLE, result1.status());
}

TEST_F(CheckCacheTest, TestWithInvalidReferenced) {
  CheckCache::CheckResult result;
  cache_->Check(attributes_, &result);
  EXPECT_FALSE(result.IsCacheHit());

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  auto match = ok_response.mutable_precondition()
                   ->mutable_referenced_attributes()
                   ->add_attribute_matches();
  match->set_condition(ReferencedAttributes::ABSENCE);
  match->set_name(10000);  // global index is too big

  // The status for the current check is still OK
  result.SetResponse(Status::OK, attributes_, ok_response);
  EXPECT_OK(result.status());

  // Since previous result was not saved to cache, this should not be cache hit
  CheckCache::CheckResult result1;
  cache_->Check(attributes_, &result1);
  EXPECT_FALSE(result1.IsCacheHit());
}

TEST_F(CheckCacheTest, TestWithMismatchedReferenced) {
  CheckCache::CheckResult result;
  cache_->Check(attributes_, &result);
  EXPECT_FALSE(result.IsCacheHit());

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  // Requires target.service to be absence
  // But this attribute is in the request, the response is not saved to cache
  auto match = ok_response.mutable_precondition()
                   ->mutable_referenced_attributes()
                   ->add_attribute_matches();
  match->set_condition(ReferencedAttributes::ABSENCE);
  match->set_name(9);

  // The status for the current check is still OK
  result.SetResponse(Status::OK, attributes_, ok_response);
  EXPECT_OK(result.status());

  // Since previous result was not saved to cache, this should not be cache hit
  CheckCache::CheckResult result1;
  cache_->Check(attributes_, &result1);
  EXPECT_FALSE(result1.IsCacheHit());
}

TEST_F(CheckCacheTest, TestTwoCacheKeys) {
  CheckCache::CheckResult result;
  cache_->Check(attributes_, &result);
  EXPECT_FALSE(result.IsCacheHit());

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  auto match = ok_response.mutable_precondition()
                   ->mutable_referenced_attributes()
                   ->add_attribute_matches();
  match->set_condition(ReferencedAttributes::EXACT);
  match->set_name(9);  // target.service is used.

  result.SetResponse(Status::OK, attributes_, ok_response);
  EXPECT_OK(result.status());

  // Cached response is used.
  CheckCache::CheckResult result1;
  cache_->Check(attributes_, &result1);
  EXPECT_TRUE(result1.IsCacheHit());

  Attributes attributes1;
  attributes1.attributes["target.service"] =
      Attributes::StringValue("different target service");

  // Not in the cache since it has different value
  CheckCache::CheckResult result2;
  cache_->Check(attributes1, &result2);
  EXPECT_FALSE(result2.IsCacheHit());

  // Store the response to the cache
  result2.SetResponse(Status::OK, attributes1, ok_response);
  EXPECT_OK(result.status());

  // Now it should be in the cache.
  CheckCache::CheckResult result3;
  cache_->Check(attributes1, &result3);
  EXPECT_TRUE(result3.IsCacheHit());

  // Also make sure key1 still in the cache
  CheckCache::CheckResult result4;
  cache_->Check(attributes_, &result4);
  EXPECT_TRUE(result4.IsCacheHit());
}

TEST_F(CheckCacheTest, TestTwoReferenced) {
  CheckCache::CheckResult result;
  cache_->Check(attributes_, &result);
  EXPECT_FALSE(result.IsCacheHit());

  CheckResponse ok_response;
  ok_response.mutable_precondition()->set_valid_use_count(1000);
  auto match = ok_response.mutable_precondition()
                   ->mutable_referenced_attributes()
                   ->add_attribute_matches();
  match->set_condition(ReferencedAttributes::EXACT);
  match->set_name(9);  // target.service is used.
  result.SetResponse(Status::OK, attributes_, ok_response);

  Attributes attributes1;
  attributes1.attributes["target.name"] =
      Attributes::StringValue("target name");

  // Not in the cache since it has different value
  CheckCache::CheckResult result1;
  cache_->Check(attributes1, &result1);
  EXPECT_FALSE(result1.IsCacheHit());

  // Store the response to the cache
  CheckResponse ok_response1;
  ok_response1.mutable_precondition()->set_valid_use_count(1000);
  auto match1 = ok_response1.mutable_precondition()
                    ->mutable_referenced_attributes()
                    ->add_attribute_matches();
  match1->set_condition(ReferencedAttributes::EXACT);
  match1->set_name(10);  // target.name is used.
  result1.SetResponse(Status::OK, attributes1, ok_response1);

  // Now both should be in the cache.
  CheckCache::CheckResult result2;
  cache_->Check(attributes_, &result2);
  EXPECT_TRUE(result2.IsCacheHit());

  CheckCache::CheckResult result3;
  cache_->Check(attributes1, &result3);
  EXPECT_TRUE(result3.IsCacheHit());
}

}  // namespace mixer_client
}  // namespace istio
