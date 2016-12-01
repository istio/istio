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
#include "src/api_manager/auth/lib/base64.h"
#include "gtest/gtest.h"
#include "src/api_manager/auth/lib/auth_token.h"

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
