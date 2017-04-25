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

using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::Invoke;
using ::testing::_;

namespace istio {
namespace mixer_client {
namespace {

class MockQuotaTransport : public QuotaTransport {
 public:
  MockQuotaTransport() : QuotaTransport(nullptr) {}

  MOCK_METHOD3_T(Send,
                 void(const Attributes&, QuotaResponse*, DoneFunc on_done));
};

// Return empty if not set.
std::string GetQuotaName(const Attributes& request) {
  const auto& it = request.attributes.find(Attributes::kQuotaName);
  if (it != request.attributes.end()) {
    return it->second.str_v;
  } else {
    return "";
  }
}

// Return -1 if not set.
int GetQuotaAmount(const Attributes& request) {
  const auto& it = request.attributes.find(Attributes::kQuotaAmount);
  if (it != request.attributes.end()) {
    return it->second.value.int64_v;
  } else {
    return -1;
  }
}

class QuotaCacheTest : public ::testing::Test {
 public:
  void SetUp() {
    QuotaOptions options;
    cache_ =
        std::unique_ptr<QuotaCache>(new QuotaCache(options, &mock_transport_));
    ASSERT_TRUE((bool)(cache_));

    request_.attributes[Attributes::kQuotaName] =
        Attributes::StringValue("RequestCount");
  }

  Attributes request_;
  MockQuotaTransport mock_transport_;
  std::unique_ptr<QuotaCache> cache_;
};

TEST_F(QuotaCacheTest, TestNullTransport) {
  QuotaOptions options;
  cache_ = std::unique_ptr<QuotaCache>(new QuotaCache(options, nullptr));
  ASSERT_TRUE((bool)(cache_));

  cache_->Quota(request_, [](const Status& status) {
    EXPECT_ERROR_CODE(Code::INVALID_ARGUMENT, status);
  });
}

TEST_F(QuotaCacheTest, TestEmptyRequest) {
  Attributes empty_request;
  cache_->Quota(empty_request, [](const Status& status) {
    EXPECT_ERROR_CODE(Code::INVALID_ARGUMENT, status);
  });
}

TEST_F(QuotaCacheTest, TestDisabledCache) {
  // A disabled cache
  QuotaOptions options(0, 1000);
  cache_ =
      std::unique_ptr<QuotaCache>(new QuotaCache(options, &mock_transport_));
  ASSERT_TRUE((bool)(cache_));

  // Not use prefetch, calling transport directly.
  EXPECT_CALL(mock_transport_, Send(_, _, _))
      .WillOnce(Invoke([this](const Attributes& request,
                              QuotaResponse* response, DoneFunc on_done) {
        EXPECT_EQ(GetQuotaName(request), GetQuotaName(request_));
        EXPECT_EQ(GetQuotaAmount(request), GetQuotaAmount(request_));
        on_done(Status(Code::INTERNAL, ""));
      }));

  cache_->Quota(request_, [](const Status& status) { EXPECT_OK(status); });
}

TEST_F(QuotaCacheTest, TestCacheKeyWithDifferentNames) {
  // Use prefetch.
  // Expect Send to be called twice, different names should use
  // different cache items.
  std::vector<DoneFunc> on_done_vect;
  EXPECT_CALL(mock_transport_, Send(_, _, _))
      .WillOnce(Invoke([&](const Attributes& request, QuotaResponse* response,
                           DoneFunc on_done) {
        EXPECT_EQ(GetQuotaName(request), "Name1");
        // prefetch amount should be > 1
        EXPECT_GT(GetQuotaAmount(request), 1);
        on_done_vect.push_back(on_done);
      }))
      .WillOnce(Invoke([&](const Attributes& request, QuotaResponse* response,
                           DoneFunc on_done) {
        EXPECT_EQ(GetQuotaName(request), "Name2");
        // prefetch amount should be > 1
        EXPECT_GT(GetQuotaAmount(request), 1);
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
  EXPECT_CALL(mock_transport_, Send(_, _, _))
      .WillOnce(Invoke([&](const Attributes& request, QuotaResponse* response,
                           DoneFunc on_done) {
        EXPECT_EQ(GetQuotaName(request), "RequestCount");
        // prefetch amount should be > 1
        EXPECT_GT(GetQuotaAmount(request), 1);
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
