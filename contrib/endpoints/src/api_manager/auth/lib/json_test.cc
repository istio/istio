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
#include "src/api_manager/auth/lib/json.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {
namespace auth {

TEST(EspJsonTest, NormalDataTest) {
  UserInfo user_info{"id", "email", "consumer_id", "iss", {"aud"}};
  static const char expected_json[] =
      "{\"issuer\":\"iss\",\"id\":\"id\",\"email\":\"email\",\"consumer_id\":"
      "\"consumer_id\"}";

  ASSERT_STREQ(expected_json, WriteUserInfoToJson(user_info));
}

TEST(EspJsonTest, DoubleQuoteTest) {
  UserInfo user_info{"id", "email \"with\" quote", "consumer_id", "iss", {}};
  static const char expected_json[] =
      "{\"issuer\":\"iss\",\"id\":\"id\",\"email\":\"email \\\"with\\\" "
      "quote\",\"consumer_id\":\"consumer_id\"}";

  ASSERT_STREQ(expected_json, WriteUserInfoToJson(user_info));
}

TEST(EspJsonTest, SingleQuoteTest) {
  UserInfo user_info{"id", "email 'with' quote", "consumer_id", "iss", {}};
  static const char expected_json[] =
      "{\"issuer\":\"iss\",\"id\":\"id\",\"email\":\"email 'with' "
      "quote\",\"consumer_id\":\"consumer_id\"}";

  ASSERT_STREQ(expected_json, WriteUserInfoToJson(user_info));
}

TEST(EspJsonTest, SlashTest) {
  UserInfo user_info{"id", "email \\with\\ quote", "consumer_id", "iss", {}};
  static const char expected_json[] =
      "{\"issuer\":\"iss\",\"id\":\"id\",\"email\":\"email \\\\with\\\\ "
      "quote\",\"consumer_id\":\"consumer_id\"}";

  ASSERT_STREQ(expected_json, WriteUserInfoToJson(user_info));
}

}  // namespace auth
}  // namespace api_manager
}  // namespace google
