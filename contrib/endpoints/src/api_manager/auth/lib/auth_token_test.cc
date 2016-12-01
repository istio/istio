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
#include "src/api_manager/auth/lib/auth_token.h"
#include "gtest/gtest.h"

#include <string.h>

namespace google {
namespace api_manager {
namespace auth {
namespace {

class AuthTokenTest : public ::testing::Test {
 protected:
  char *good_token_;
  char *empty_token_;
  char *missing_access_token_;
  char *missing_expires_in_token_;
  char *bad_access_token_type_;
  char *bad_expires_in_type_;

  void SetUp() {
    good_token_ = strdup(good_token);
    empty_token_ = strdup(empty_token);
    missing_access_token_ = strdup(missing_access_token);
    missing_expires_in_token_ = strdup(missing_expires_in_token);
    bad_access_token_type_ = strdup(bad_access_token_type);
    bad_expires_in_type_ = strdup(bad_expires_in_type);
  }

  void TearDown() {
    free(good_token_);
    free(empty_token_);
    free(missing_access_token_);
    free(missing_expires_in_token_);
    free(bad_access_token_type_);
    free(bad_expires_in_type_);
  }

 private:
  static const char good_token[];
  static const char empty_token[];
  static const char missing_access_token[];
  static const char missing_expires_in_token[];
  static const char bad_access_token_type[];
  static const char bad_expires_in_type[];
};

const char AuthTokenTest::good_token[] =
    "{"
    "  \"access_token\":"
    "  \"ya29.AHES6ZRN3-HlhAPya30GnW_bHSb_QtAS08i85nHq39HE3C2LTrCARA\","
    "  \"expires_in\":3599,"
    "  \"token_type\":\"Bearer\""
    "}";

const char AuthTokenTest::empty_token[] = "{ }";

const char AuthTokenTest::missing_access_token[] =
    "{"
    "  \"expires_in\":3599,"
    "  \"token_type\":\"Bearer\""
    "}";

const char AuthTokenTest::missing_expires_in_token[] =
    "{"
    "  \"access_token\":"
    "  \"ya29.AHES6ZRN3-HlhAPya30GnW_bHSb_QtAS08i85nHq39HE3C2LTrCARA\","
    "  \"token_type\":\"Bearer\""
    "}";

const char AuthTokenTest::bad_access_token_type[] =
    "{"
    "  \"access_token\":1234,"
    "  \"expires_in\":3599,"
    "  \"token_type\":\"Bearer\""
    "}";

const char AuthTokenTest::bad_expires_in_type[] =
    "{"
    "  \"access_token\":"
    "  \"ya29.AHES6ZRN3-HlhAPya30GnW_bHSb_QtAS08i85nHq39HE3C2LTrCARA\","
    "  \"expires_in\":\"3599\","
    "  \"token_type\":\"Bearer\""
    "}";

TEST_F(AuthTokenTest, ParseServiceAccountToken) {
  char *token = nullptr;
  int expires = 0;

  ASSERT_TRUE(esp_get_service_account_auth_token(
      good_token_, strlen(good_token_), &token, &expires));
  ASSERT_STREQ("ya29.AHES6ZRN3-HlhAPya30GnW_bHSb_QtAS08i85nHq39HE3C2LTrCARA",
               token);
  ASSERT_EQ(3599, expires);
}

TEST_F(AuthTokenTest, ParseEmptyServiceAccountToken) {
  char *token = nullptr;
  int expires = 0;

  ASSERT_FALSE(esp_get_service_account_auth_token(
      empty_token_, strlen(empty_token_), &token, &expires));
  ASSERT_EQ(nullptr, token);
  ASSERT_EQ(0, expires);
}

TEST_F(AuthTokenTest, ParseMissingAccessToken) {
  char *token = nullptr;
  int expires = 0;

  ASSERT_FALSE(esp_get_service_account_auth_token(
      missing_access_token_, strlen(missing_access_token_), &token, &expires));
  ASSERT_EQ(nullptr, token);
  ASSERT_EQ(0, expires);
}

TEST_F(AuthTokenTest, ParseMissingExpiresIn) {
  char *token = nullptr;
  int expires = 0;

  ASSERT_FALSE(esp_get_service_account_auth_token(
      missing_expires_in_token_, strlen(missing_expires_in_token_), &token,
      &expires));
  ASSERT_EQ(nullptr, token);
  ASSERT_EQ(0, expires);
}

TEST_F(AuthTokenTest, ParseBadAccessTokenType) {
  char *token = nullptr;
  int expires = 0;

  ASSERT_FALSE(esp_get_service_account_auth_token(
      bad_access_token_type_, strlen(bad_access_token_type_), &token,
      &expires));
  ASSERT_EQ(nullptr, token);
  ASSERT_EQ(0, expires);
}

TEST_F(AuthTokenTest, ParseBadExpiresInType) {
  char *token = nullptr;
  int expires = 0;

  ASSERT_FALSE(esp_get_service_account_auth_token(
      bad_expires_in_type_, strlen(bad_expires_in_type_), &token, &expires));
  ASSERT_EQ(nullptr, token);
  ASSERT_EQ(0, expires);
}

TEST_F(AuthTokenTest, ParseNullServiceAccountToken) {
  char *token = nullptr;
  int expires = 0;

  ASSERT_FALSE(
      esp_get_service_account_auth_token(nullptr, 0, &token, &expires));
  ASSERT_EQ(nullptr, token);
  ASSERT_EQ(0, expires);
}

}  // namespace
}  // namespace auth
}  // namespace api_manager
}  // namespace google
