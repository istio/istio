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
#include "src/api_manager/auth/service_account_token.h"

#include "gtest/gtest.h"
#include "src/api_manager/mock_api_manager_environment.h"

using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace auth {

namespace {

class ServiceAccountTokenTest : public ::testing::Test {
 public:
  void SetUp() {
    env_.reset(new ::testing::NiceMock<MockApiManagerEnvironmentWithLog>());
    sa_token_ = std::unique_ptr<ServiceAccountToken>(
        new ServiceAccountToken(env_.get()));
  }

  std::unique_ptr<ApiManagerEnvInterface> env_;
  std::unique_ptr<ServiceAccountToken> sa_token_;
};

TEST_F(ServiceAccountTokenTest, TestAccessToken) {
  // Needed.
  ASSERT_FALSE(sa_token_->is_access_token_valid(0));
  // Adds a token, not needed now
  sa_token_->set_access_token("Dummy Token", 1);
  ASSERT_TRUE(sa_token_->is_access_token_valid(0));
  ASSERT_EQ("Dummy Token",
            sa_token_->GetAuthToken(
                ServiceAccountToken::JWT_TOKEN_FOR_SERVICE_CONTROL));

  sleep(2);
  // Token is expired, needed now.
  ASSERT_FALSE(sa_token_->is_access_token_valid(0));
  // Returns expired token.
  ASSERT_EQ("Dummy Token",
            sa_token_->GetAuthToken(
                ServiceAccountToken::JWT_TOKEN_FOR_SERVICE_CONTROL));
}

TEST_F(ServiceAccountTokenTest, TestClientAuthSecret) {
  // Needed.
  ASSERT_FALSE(sa_token_->is_access_token_valid(0));
  ASSERT_EQ(ServiceAccountToken::NONE, sa_token_->state());

  sa_token_->SetAudience(ServiceAccountToken::JWT_TOKEN_FOR_SERVICE_CONTROL,
                         "audience");
  Status status = sa_token_->SetClientAuthSecret("Dummy secret");
  ASSERT_EQ(status.code(), Code::INVALID_ARGUMENT);

  // As long as there is a client auth secret. no need to get token.
  ASSERT_TRUE(sa_token_->has_client_secret());
  // Returns empty string for an invalid secret.
  ASSERT_EQ("", sa_token_->GetAuthToken(
                    ServiceAccountToken::JWT_TOKEN_FOR_SERVICE_CONTROL));
}

}  // namespace

}  // namespace auth
}  // namespace api_manager
}  // namespace google
