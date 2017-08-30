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

#include "src/quota_cache.h"

#include "gmock/gmock.h"
#include "gtest/gtest.h"
#include "utils/status_test_util.h"

using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::Invoke;
using ::testing::_;

namespace istio {
namespace mixer_client {
namespace {

const std::string kQuotaName = "RequestCount";

class QuotaCacheTest : public ::testing::Test {
 public:
  void SetUp() {
    QuotaOptions options;
    cache_ = std::unique_ptr<QuotaCache>(new QuotaCache(options));
    ASSERT_TRUE((bool)(cache_));

    request_.attributes[Attributes::kQuotaName] =
        Attributes::StringValue(kQuotaName);
  }

  Attributes request_;
  std::unique_ptr<QuotaCache> cache_;
};

TEST_F(QuotaCacheTest, TestEmptyRequest) {
  // Quota is not required.
  Attributes empty_request;
  QuotaCache::CheckResult result;
  cache_->Check(empty_request, true, &result);

  CheckRequest request;
  EXPECT_FALSE(result.BuildRequest(&request));
  EXPECT_TRUE(result.IsCacheHit());
  EXPECT_OK(result.status());
  EXPECT_EQ(request.quotas().size(), 0);
}

TEST_F(QuotaCacheTest, TestDisabledCache) {
  // A disabled cache
  QuotaOptions options(0, 1000);
  cache_ = std::unique_ptr<QuotaCache>(new QuotaCache(options));
  ASSERT_TRUE((bool)(cache_));

  QuotaCache::CheckResult result;
  cache_->Check(request_, true, &result);

  CheckRequest request;
  EXPECT_TRUE(result.BuildRequest(&request));
  EXPECT_FALSE(result.IsCacheHit());

  EXPECT_EQ(request.quotas().size(), 1);
  EXPECT_EQ(request.quotas().begin()->first, kQuotaName);
  EXPECT_EQ(request.quotas().begin()->second.amount(), 1);
  EXPECT_EQ(request.quotas().begin()->second.best_effort(), false);

  CheckResponse response;
  CheckResponse::QuotaResult quota_result;
  quota_result.set_granted_amount(1);
  (*response.mutable_quotas())[kQuotaName] = quota_result;
  result.SetResponse(Status::OK, request_, response);
  EXPECT_OK(result.status());
}

TEST_F(QuotaCacheTest, TestNotUseCache) {
  QuotaCache::CheckResult result;
  cache_->Check(request_, false, &result);

  CheckRequest request;
  EXPECT_TRUE(result.BuildRequest(&request));
  EXPECT_FALSE(result.IsCacheHit());

  EXPECT_EQ(request.quotas().size(), 1);
  EXPECT_EQ(request.quotas().begin()->first, kQuotaName);
  EXPECT_EQ(request.quotas().begin()->second.amount(), 1);
  EXPECT_EQ(request.quotas().begin()->second.best_effort(), false);

  CheckResponse response;
  CheckResponse::QuotaResult quota_result;
  // granted_amount = 0
  quota_result.set_granted_amount(0);
  (*response.mutable_quotas())[kQuotaName] = quota_result;
  result.SetResponse(Status::OK, request_, response);
  EXPECT_ERROR_CODE(Code::RESOURCE_EXHAUSTED, result.status());
}

TEST_F(QuotaCacheTest, TestUseCache) {
  QuotaCache::CheckResult result;
  cache_->Check(request_, true, &result);

  CheckRequest request;
  EXPECT_TRUE(result.BuildRequest(&request));

  // Prefetch always allow the first call.
  EXPECT_TRUE(result.IsCacheHit());
  EXPECT_OK(result.status());

  // Then try to prefetch some.
  EXPECT_EQ(request.quotas().size(), 1);
  EXPECT_EQ(request.quotas().begin()->first, kQuotaName);
  // Prefetch amount should be > 1
  EXPECT_GT(request.quotas().begin()->second.amount(), 1);
  EXPECT_EQ(request.quotas().begin()->second.best_effort(), true);

  CheckResponse response;
  result.SetResponse(Status::OK, request_, response);
  EXPECT_OK(result.status());
}

TEST_F(QuotaCacheTest, TestUseCacheRejected) {
  CheckResponse response;
  CheckResponse::QuotaResult quota_result;
  // Not more quota.
  quota_result.set_granted_amount(0);
  (*response.mutable_quotas())[kQuotaName] = quota_result;

  int rejected = 0;
  for (int i = 0; i < 10; i++) {
    QuotaCache::CheckResult result;
    cache_->Check(request_, true, &result);

    CheckRequest request;
    result.BuildRequest(&request);

    // Prefetch always allow the first call.
    EXPECT_TRUE(result.IsCacheHit());
    if (!result.status().ok()) {
      ++rejected;
    }

    result.SetResponse(Status::OK, request_, response);
  }
  // Only the first one allowed, the rest should be rejected.
  EXPECT_EQ(rejected, 9);
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
