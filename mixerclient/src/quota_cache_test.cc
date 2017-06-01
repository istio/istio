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

using ::istio::mixer::v1::QuotaRequest;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::Invoke;
using ::testing::_;

namespace istio {
namespace mixer_client {
namespace {

const std::string kQuotaName = "RequestCount";

// A mocking class to mock CheckTransport interface.
class MockQuotaTransport {
 public:
  MOCK_METHOD3(Quota, void(const QuotaRequest&, QuotaResponse*, DoneFunc));
  TransportQuotaFunc GetFunc() {
    return
        [this](const QuotaRequest& request, QuotaResponse* response,
               DoneFunc on_done) { this->Quota(request, response, on_done); };
  }
};

class QuotaCacheTest : public ::testing::Test {
 public:
  QuotaCacheTest() : converter_({}) {}

  void SetUp() {
    QuotaOptions options;
    cache_ = std::unique_ptr<QuotaCache>(
        new QuotaCache(options, mock_transport_.GetFunc(), converter_));
    ASSERT_TRUE((bool)(cache_));

    request_.attributes[Attributes::kQuotaName] =
        Attributes::StringValue(kQuotaName);
  }

  Attributes request_;
  MockQuotaTransport mock_transport_;
  AttributeConverter converter_;
  std::unique_ptr<QuotaCache> cache_;
};

TEST_F(QuotaCacheTest, TestEmptyRequest) {
  Attributes empty_request;
  cache_->Quota(empty_request, [](const Status& status) {
    EXPECT_ERROR_CODE(Code::INVALID_ARGUMENT, status);
  });
}

TEST_F(QuotaCacheTest, TestDisabledCache) {
  // A disabled cache
  QuotaOptions options(0, 1000);
  cache_ = std::unique_ptr<QuotaCache>(
      new QuotaCache(options, mock_transport_.GetFunc(), converter_));
  ASSERT_TRUE((bool)(cache_));

  // Not use prefetch, calling transport directly.
  EXPECT_CALL(mock_transport_, Quota(_, _, _))
      .WillOnce(Invoke([this](const QuotaRequest& request,
                              QuotaResponse* response, DoneFunc on_done) {
        EXPECT_EQ(request.quota(), kQuotaName);
        EXPECT_EQ(request.amount(), 1);
        on_done(Status(Code::INTERNAL, ""));
      }));

  cache_->Quota(request_, [](const Status& status) {
    EXPECT_ERROR_CODE(Code::INTERNAL, status);
  });
}

TEST_F(QuotaCacheTest, TestCacheKeyWithDifferentNames) {
  // Use prefetch.
  // Expect Send to be called twice, different names should use
  // different cache items.
  std::vector<DoneFunc> on_done_vect;
  EXPECT_CALL(mock_transport_, Quota(_, _, _))
      .WillOnce(Invoke([&](const QuotaRequest& request, QuotaResponse* response,
                           DoneFunc on_done) {
        EXPECT_EQ(request.quota(), "Name1");
        // prefetch amount should be > 1
        EXPECT_GT(request.amount(), 1);
        on_done_vect.push_back(on_done);
      }))
      .WillOnce(Invoke([&](const QuotaRequest& request, QuotaResponse* response,
                           DoneFunc on_done) {
        EXPECT_EQ(request.quota(), "Name2");
        // prefetch amount should be > 1
        EXPECT_GT(request.amount(), 1);
        on_done_vect.push_back(on_done);
      }));

  request_.attributes[Attributes::kQuotaName] =
      Attributes::StringValue("Name1");
  cache_->Quota(request_, [](const Status& status) {
    // Prefetch request is sucesss
    EXPECT_OK(status);
  });

  request_.attributes[Attributes::kQuotaName] =
      Attributes::StringValue("Name2");
  cache_->Quota(request_, [](const Status& status) {
    // Prefetch request is sucesss
    EXPECT_OK(status);
  });

  // Call all on_done to clean up the memory.
  for (const auto& on_done : on_done_vect) {
    on_done(Status::OK);
  }
}

TEST_F(QuotaCacheTest, TestCacheKeyWithDifferentAmounts) {
  // Use prefetch.
  // Expect Send to be called once, same name different amounts should
  // share same cache.
  std::vector<DoneFunc> on_done_vect;
  EXPECT_CALL(mock_transport_, Quota(_, _, _))
      .WillOnce(Invoke([&](const QuotaRequest& request, QuotaResponse* response,
                           DoneFunc on_done) {
        EXPECT_EQ(request.quota(), kQuotaName);
        // prefetch amount should be > 1
        EXPECT_GT(request.amount(), 1);
        on_done_vect.push_back(on_done);
      }));

  request_.attributes[Attributes::kQuotaAmount] = Attributes::Int64Value(1);
  cache_->Quota(request_, [](const Status& status) {
    // Prefetch request is sucesss
    EXPECT_OK(status);
  });

  request_.attributes[Attributes::kQuotaAmount] = Attributes::Int64Value(2);
  cache_->Quota(request_, [](const Status& status) {
    // Prefetch request is sucesss
    EXPECT_OK(status);
  });

  // Call all on_done to clean up the memory.
  for (const auto& on_done : on_done_vect) {
    on_done(Status::OK);
  }
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
