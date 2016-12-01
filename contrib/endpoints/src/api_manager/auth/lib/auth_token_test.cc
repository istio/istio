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
