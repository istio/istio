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
#include "utils/status_test_util.h"

#include <unistd.h>

using std::string;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {
namespace {

const int kFlushIntervalMs = 100;
const int kExpirationMs = 200;
}

class CheckCacheTest : public ::testing::Test {
 public:
  void SetUp() {
    CheckOptions options(1 /*entries*/, kFlushIntervalMs, kExpirationMs);
    options.cache_keys = {"string-key"};

    cache_ = std::unique_ptr<CheckCache>(new CheckCache(options));
    ASSERT_TRUE((bool)(cache_));

    attributes1_.attributes["string-key"] =
        Attributes::StringValue("this-is-a-string-value");
  }

  Attributes attributes1_;

  std::unique_ptr<CheckCache> cache_;
};

TEST_F(CheckCacheTest, TestDisableCache1) {
  // 0 cache entries. cache is disabled
  CheckOptions options(0 /*entries*/, 1000, 2000);
  options.cache_keys = {"string-key"};
  cache_ = std::unique_ptr<CheckCache>(new CheckCache(options));

  ASSERT_TRUE((bool)(cache_));

  EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes1_, nullptr));
}

TEST_F(CheckCacheTest, TestDisableCache2) {
  // empty cache keys. cache is disabled
  CheckOptions options(1000 /*entries*/, 1000, 2000);
  cache_ = std::unique_ptr<CheckCache>(new CheckCache(options));

  ASSERT_TRUE((bool)(cache_));

  EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes1_, nullptr));
}

TEST_F(CheckCacheTest, TestCachePassResponses) {
  std::string signature;
  EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes1_, &signature));

  EXPECT_OK(cache_->CacheResponse(signature, Status::OK));
  EXPECT_OK(cache_->Check(attributes1_, &signature));
}

TEST_F(CheckCacheTest, TestRefresh) {
  std::string signature;
  EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes1_, &signature));

  EXPECT_OK(cache_->CacheResponse(signature, Status::OK));
  EXPECT_OK(cache_->Check(attributes1_, &signature));
  // sleep 0.12 second.
  usleep(120000);

  // First one should be NOT_FOUND for refresh
  EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes1_, &signature));
  // Second one use cached response.
  EXPECT_OK(cache_->CacheResponse(signature, Status::OK));
  EXPECT_OK(cache_->Check(attributes1_, &signature));

  EXPECT_OK(cache_->FlushAll());
  EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes1_, &signature));
}

TEST_F(CheckCacheTest, TestCacheExpired) {
  std::string signature;
  EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes1_, &signature));

  EXPECT_OK(cache_->CacheResponse(signature, Status::OK));
  EXPECT_OK(cache_->Check(attributes1_, &signature));

  // sleep 0.22 second to cause cache expired.
  usleep(220000);
  EXPECT_OK(cache_->Flush());

  // First one should be NOT_FOUND for refresh
  EXPECT_ERROR_CODE(Code::NOT_FOUND, cache_->Check(attributes1_, &signature));
}

}  // namespace mixer_client
}  // namespace istio
