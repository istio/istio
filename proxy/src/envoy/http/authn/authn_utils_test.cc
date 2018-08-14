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
#include "common/common/utility.h"
#include "src/envoy/http/authn/test_utils.h"
#include "test/test_common/utility.h"

using google::protobuf::util::MessageDifferencer;
using istio::authn::JwtPayload;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {
namespace {

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

TEST(AuthnUtilsTest, GetJwtPayloadFromHeaderTest) {
  JwtPayload payload, expected_payload;
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
      raw_claims: ")" +
          StringUtil::escape(kSecIstioAuthUserinfoHeaderValue) + R"(")",
      &expected_payload));
  // The payload returned from ProcessJwtPayload() should be the same as
  // the expected.
  bool ret =
      AuthnUtils::ProcessJwtPayload(kSecIstioAuthUserinfoHeaderValue, &payload);
  EXPECT_TRUE(ret);
  EXPECT_TRUE(MessageDifferencer::Equals(expected_payload, payload));
}

TEST(AuthnUtilsTest, ProcessJwtPayloadWithNoAudTest) {
  JwtPayload payload, expected_payload;
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
      raw_claims: ")" +
          StringUtil::escape(kSecIstioAuthUserInfoHeaderWithNoAudValue) +
          R"(")",
      &expected_payload));
  // The payload returned from ProcessJwtPayload() should be the same as
  // the expected. When there is no aud,  the aud is not saved in the payload
  // and claims.
  bool ret = AuthnUtils::ProcessJwtPayload(
      kSecIstioAuthUserInfoHeaderWithNoAudValue, &payload);
  EXPECT_TRUE(ret);
  EXPECT_TRUE(MessageDifferencer::Equals(expected_payload, payload));
}

TEST(AuthnUtilsTest, ProcessJwtPayloadWithTwoAudTest) {
  JwtPayload payload, expected_payload;
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
      raw_claims: ")" +
          StringUtil::escape(kSecIstioAuthUserInfoHeaderWithTwoAudValue) +
          R"(")",
      &expected_payload));

  // The payload returned from ProcessJwtPayload() should be the same as
  // the expected. When the aud is a string array, the aud is not saved in the
  // claims.
  bool ret = AuthnUtils::ProcessJwtPayload(
      kSecIstioAuthUserInfoHeaderWithTwoAudValue, &payload);
  EXPECT_TRUE(ret);
  EXPECT_TRUE(MessageDifferencer::Equals(expected_payload, payload));
}

}  // namespace
}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
