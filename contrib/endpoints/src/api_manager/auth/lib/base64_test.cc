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
#include "contrib/endpoints/src/api_manager/auth/lib/base64.h"
#include "contrib/endpoints/src/api_manager/auth/lib/auth_token.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {
namespace auth {

struct TestVector {
  const char *data;
  const char *encoded_padding;
  const char *encoded_no_padding;
};

/* Test vectors from RFC 4648. */
static const TestVector test_vectors[] = {
    {"", "", ""},
    {"f", "Zg==", "Zg"},
    {"fo", "Zm8=", "Zm8"},
    {"foo", "Zm9v", "Zm9v"},
    {"foob", "Zm9vYg==", "Zm9vYg"},
    {"fooba", "Zm9vYmE=", "Zm9vYmE"},
    {"foobar", "Zm9vYmFy", "Zm9vYmFy"},
};

TEST(EspBase64Test, EncodeTest) {
  for (const auto &t : test_vectors) {
    char *encoded_padding =
        esp_base64_encode(t.data, strlen(t.data), true, false, true);
    ASSERT_NE(nullptr, encoded_padding);
    ASSERT_STREQ(t.encoded_padding, encoded_padding);
    esp_grpc_free(encoded_padding);

    char *encoded_no_padding =
        esp_base64_encode(t.data, strlen(t.data), true, false, false);
    ASSERT_NE(nullptr, encoded_no_padding);
    ASSERT_STREQ(t.encoded_no_padding, encoded_no_padding);
    esp_grpc_free(encoded_no_padding);
  }
}

}  // namespace auth
}  // namespace api_manager
}  // namespace google
