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
