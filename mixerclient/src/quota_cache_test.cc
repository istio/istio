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
using ::istio::mixer::v1::ReferencedAttributes;
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

  void TestRequest(const Attributes& request, bool pass,
                   const CheckResponse& response) {
    QuotaCache::CheckResult result;
    cache_->Check(request, true, &result);

    CheckRequest request_pb;
    result.BuildRequest(&request_pb);

    EXPECT_TRUE(result.IsCacheHit());
    if (pass) {
      EXPECT_TRUE(result.status().ok());
    } else {
      EXPECT_FALSE(result.status().ok());
    }

    result.SetResponse(Status::OK, request, response);
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

TEST_F(QuotaCacheTest, TestInvalidQuotaReferenced) {
  // If quota result Referenced is invalid (wrong word index),
  // its cache item stays in pending.
  // Other cache miss requests with same quota name will use pending
  // item.
  CheckResponse response;
  CheckResponse::QuotaResult quota_result;
  // Not more quota.
  quota_result.set_granted_amount(0);
  auto match =
      quota_result.mutable_referenced_attributes()->add_attribute_matches();
  match->set_condition(ReferencedAttributes::ABSENCE);
  match->set_name(10000);  // global index is too big
  (*response.mutable_quotas())[kQuotaName] = quota_result;

  Attributes attr(request_);
  attr.attributes["source.name"] = Attributes::StringValue("user1");
  // response has invalid referenced, cache item still in pending.
  TestRequest(attr, true, response);

  attr.attributes["source.name"] = Attributes::StringValue("user2");
  // it is a cache miss, use pending request.
  // Previous request has used up token, this request will be rejected.
  TestRequest(attr, false, response);
}

TEST_F(QuotaCacheTest, TestMismatchedReferenced) {
  // If quota result Referenced is mismatched with request data.
  // its cache item stays in pending.
  // Other cache miss requests with same quota name will use pending
  // item.
  CheckResponse response;
  CheckResponse::QuotaResult quota_result;
  // Not more quota.
  quota_result.set_granted_amount(0);
  auto match =
      quota_result.mutable_referenced_attributes()->add_attribute_matches();
  match->set_condition(ReferencedAttributes::ABSENCE);
  match->set_name(2);  // "source.name" should be absence (mismatch)
  (*response.mutable_quotas())[kQuotaName] = quota_result;

  Attributes attr(request_);
  attr.attributes["source.name"] = Attributes::StringValue("user1");
  // Since respones has mismatched Referenced, cache item still in pending.
  // Prefetch always allow the first call.
  TestRequest(attr, true, response);

  // second request with different users still use the pending request.
  attr.attributes["source.name"] = Attributes::StringValue("user2");
  // it is a cache miss, use pending request.
  // Previous request has used up token, this request will be rejected.
  TestRequest(attr, false, response);
}

TEST_F(QuotaCacheTest, TestOneReferencedWithTwoKeys) {
  // Quota needs to use source.name as cache key.
  // First source.name is exhaused, and second one is with quota.
  CheckResponse response;
  CheckResponse::QuotaResult quota_result;
  // Not more quota.
  quota_result.set_granted_amount(0);
  auto match =
      quota_result.mutable_referenced_attributes()->add_attribute_matches();
  match->set_condition(ReferencedAttributes::EXACT);
  match->set_name(2);  // "source.name" should be used
  (*response.mutable_quotas())[kQuotaName] = quota_result;

  Attributes attr1(request_);
  attr1.attributes["source.name"] = Attributes::StringValue("user1");
  Attributes attr2(request_);
  attr2.attributes["source.name"] = Attributes::StringValue("user2");

  // cache item is updated with 0 token in the pool.
  // it will be saved into cache key with user1.
  TestRequest(attr1, true, response);

  // user2 still have quota.
  quota_result.set_granted_amount(10);
  (*response.mutable_quotas())[kQuotaName] = quota_result;
  TestRequest(attr2, true, response);

  // user1 will not have quota
  TestRequest(attr1, false, response);

  // user2 will have quota
  TestRequest(attr2, true, response);
}

TEST_F(QuotaCacheTest, TestTwoReferencedWith) {
  CheckResponse::QuotaResult quota_result1;
  // Not more quota.
  quota_result1.set_granted_amount(0);
  auto match =
      quota_result1.mutable_referenced_attributes()->add_attribute_matches();
  match->set_condition(ReferencedAttributes::EXACT);
  match->set_name(2);  // "source.name" should be used
  CheckResponse response1;
  (*response1.mutable_quotas())[kQuotaName] = quota_result1;

  CheckResponse::QuotaResult quota_result2;
  quota_result2.set_granted_amount(10);
  match =
      quota_result2.mutable_referenced_attributes()->add_attribute_matches();
  match->set_condition(ReferencedAttributes::EXACT);
  match->set_name(3);  // "source.uid" should be used
  CheckResponse response2;
  (*response2.mutable_quotas())[kQuotaName] = quota_result2;

  Attributes attr1(request_);
  attr1.attributes["source.name"] = Attributes::StringValue("name");
  Attributes attr2(request_);
  attr2.attributes["source.uid"] = Attributes::StringValue("uid");

  // name request with 0 granted response
  TestRequest(attr1, true, response1);

  // uid request with 10 granted response
  TestRequest(attr2, true, response2);

  // user1 will not have quota
  TestRequest(attr1, false, response1);

  // user2 will have quota
  TestRequest(attr2, true, response2);
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
