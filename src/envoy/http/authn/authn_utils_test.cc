/* Copyright 2018 Istio Authors. All Rights Reserved.
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
#include "src/envoy/http/authn/authn_utils.h"
#include "common/common/base64.h"
#include "src/envoy/http/authn/test_utils.h"
#include "test/test_common/utility.h"

using google::protobuf::util::MessageDifferencer;
using istio::authn::JwtPayload;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {
namespace {

const LowerCaseString kSecIstioAuthUserInfoHeaderKey("sec-istio-auth-userinfo");
const std::string kSecIstioAuthUserinfoHeaderValue =
    R"(
     {
       "iss": "issuer@foo.com",
       "sub": "sub@foo.com",
       "aud": "aud1",
       "non-string-will-be-ignored": 1512754205,
       "some-other-string-claims": "some-claims-kept"
     }
   )";
const std::string kSecIstioAuthUserInfoHeaderWithNoAudValue =
    R"(
       {
         "iss": "issuer@foo.com",
         "sub": "sub@foo.com",
         "non-string-will-be-ignored": 1512754205,
         "some-other-string-claims": "some-claims-kept"
       }
     )";
const std::string kSecIstioAuthUserInfoHeaderWithTwoAudValue =
    R"(
       {
         "iss": "issuer@foo.com",
         "sub": "sub@foo.com",
         "aud": ["aud1", "aud2"],
         "non-string-will-be-ignored": 1512754205,
         "some-other-string-claims": "some-claims-kept"
       }
     )";

Http::TestHeaderMapImpl CreateTestHeaderMap(const LowerCaseString& header_key,
                                            const std::string& header_value) {
  // The base64 encoding is done through Base64::encode().
  // If the test input has special chars, may need to use the counterpart of
  // Base64UrlDecode().
  std::string value_base64 =
      Base64::encode(header_value.c_str(), header_value.size());
  return Http::TestHeaderMapImpl{{header_key.get(), value_base64}};
}

TEST(AuthnUtilsTest, GetJwtPayloadFromHeaderTest) {
  JwtPayload payload, expected_payload;
  Http::TestHeaderMapImpl request_headers_with_jwt = CreateTestHeaderMap(
      kSecIstioAuthUserInfoHeaderKey, kSecIstioAuthUserinfoHeaderValue);
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(
      R"(
      user: "issuer@foo.com/sub@foo.com"
      audiences: ["aud1"]
      claims {
        key: "aud"
        value: "aud1"
      }
      claims {
        key: "iss"
        value: "issuer@foo.com"
      }
      claims {
        key: "sub"
        value: "sub@foo.com"
      }
      claims {
        key: "some-other-string-claims"
        value: "some-claims-kept"
      }
    )",
      &expected_payload));
  // The payload returned from GetJWTPayloadFromHeaders() should be the same as
  // the expected.
  bool ret = AuthnUtils::GetJWTPayloadFromHeaders(
      request_headers_with_jwt, kSecIstioAuthUserInfoHeaderKey, &payload);
  EXPECT_TRUE(ret);
  EXPECT_TRUE(MessageDifferencer::Equals(expected_payload, payload));
}

TEST(AuthnUtilsTest, GetJwtPayloadFromHeaderWithNoAudTest) {
  JwtPayload payload, expected_payload;
  Http::TestHeaderMapImpl request_headers_with_jwt =
      CreateTestHeaderMap(kSecIstioAuthUserInfoHeaderKey,
                          kSecIstioAuthUserInfoHeaderWithNoAudValue);
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(
      R"(
      user: "issuer@foo.com/sub@foo.com"
      claims {
        key: "iss"
        value: "issuer@foo.com"
      }
      claims {
        key: "sub"
        value: "sub@foo.com"
      }
      claims {
        key: "some-other-string-claims"
        value: "some-claims-kept"
      }
    )",
      &expected_payload));
  // The payload returned from GetJWTPayloadFromHeaders() should be the same as
  // the expected. When there is no aud,  the aud is not saved in the payload
  // and claims.
  bool ret = AuthnUtils::GetJWTPayloadFromHeaders(
      request_headers_with_jwt, kSecIstioAuthUserInfoHeaderKey, &payload);
  EXPECT_TRUE(ret);
  EXPECT_TRUE(MessageDifferencer::Equals(expected_payload, payload));
}

TEST(AuthnUtilsTest, GetJwtPayloadFromHeaderWithTwoAudTest) {
  JwtPayload payload, expected_payload;
  Http::TestHeaderMapImpl request_headers_with_jwt =
      CreateTestHeaderMap(kSecIstioAuthUserInfoHeaderKey,
                          kSecIstioAuthUserInfoHeaderWithTwoAudValue);
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(
      R"(
      user: "issuer@foo.com/sub@foo.com"
      audiences: "aud1"
      audiences: "aud2"
      claims {
        key: "iss"
        value: "issuer@foo.com"
      }
      claims {
        key: "sub"
        value: "sub@foo.com"
      }
      claims {
        key: "some-other-string-claims"
        value: "some-claims-kept"
      }
    )",
      &expected_payload));

  // The payload returned from GetJWTPayloadFromHeaders() should be the same as
  // the expected. When the aud is a string array, the aud is not saved in the
  // claims.
  bool ret = AuthnUtils::GetJWTPayloadFromHeaders(
      request_headers_with_jwt, kSecIstioAuthUserInfoHeaderKey, &payload);
  EXPECT_TRUE(ret);
  EXPECT_TRUE(MessageDifferencer::Equals(expected_payload, payload));
}

}  // namespace
}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
