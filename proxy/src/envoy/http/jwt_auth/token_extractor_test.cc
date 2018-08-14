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

#include "proxy/src/envoy/http/jwt_auth/token_extractor.h"
#include "gtest/gtest.h"
#include "test/test_common/utility.h"

using ::istio::envoy::config::filter::http::jwt_auth::v2alpha1::
    JwtAuthentication;
using ::testing::Invoke;
using ::testing::NiceMock;
using ::testing::_;

namespace Envoy {
namespace Http {
namespace JwtAuth {
namespace {

const char kExampleConfig[] = R"(
{
   "rules": [
      {
         "issuer": "issuer1"
      },
      {
         "issuer": "issuer2",
         "from_headers": [
             {
                "name": "token-header"
             }
         ]
      },
      {
         "issuer": "issuer3",
         "from_params": [
             "token_param"
         ]
      },
      {
         "issuer": "issuer4",
         "from_headers": [
             {
                 "name": "token-header"
             }
         ],
         "from_params": [
             "token_param"
         ]
      }
   ]
}
)";

}  //  namespace

class JwtTokenExtractorTest : public ::testing::Test {
 public:
  void SetUp() { SetupConfig(kExampleConfig); }

  void SetupConfig(const std::string& json_str) {
    google::protobuf::util::Status status =
        ::google::protobuf::util::JsonStringToMessage(json_str, &config_);
    ASSERT_TRUE(status.ok());
    extractor_.reset(new JwtTokenExtractor(config_));
  }

  JwtAuthentication config_;
  std::unique_ptr<JwtTokenExtractor> extractor_;
};

TEST_F(JwtTokenExtractorTest, TestNoToken) {
  auto headers = TestHeaderMapImpl{};
  std::vector<std::unique_ptr<JwtTokenExtractor::Token>> tokens;
  extractor_->Extract(headers, &tokens);
  EXPECT_EQ(tokens.size(), 0);
}

TEST_F(JwtTokenExtractorTest, TestWrongHeaderToken) {
  auto headers = TestHeaderMapImpl{{"wrong-token-header", "jwt_token"}};
  std::vector<std::unique_ptr<JwtTokenExtractor::Token>> tokens;
  extractor_->Extract(headers, &tokens);
  EXPECT_EQ(tokens.size(), 0);
}

TEST_F(JwtTokenExtractorTest, TestWrongParamToken) {
  auto headers = TestHeaderMapImpl{{":path", "/path?wrong_token=jwt_token"}};
  std::vector<std::unique_ptr<JwtTokenExtractor::Token>> tokens;
  extractor_->Extract(headers, &tokens);
  EXPECT_EQ(tokens.size(), 0);
}

TEST_F(JwtTokenExtractorTest, TestDefaultHeaderLocation) {
  auto headers = TestHeaderMapImpl{{"Authorization", "Bearer jwt_token"}};
  std::vector<std::unique_ptr<JwtTokenExtractor::Token>> tokens;
  extractor_->Extract(headers, &tokens);
  EXPECT_EQ(tokens.size(), 1);
  EXPECT_EQ(tokens[0]->token(), "jwt_token");

  EXPECT_TRUE(tokens[0]->IsIssuerAllowed("issuer1"));

  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer2"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer3"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer4"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("unknown_issuer"));

  // Test token remove
  tokens[0]->Remove(&headers);
  EXPECT_FALSE(headers.Authorization());
}

TEST_F(JwtTokenExtractorTest, TestDefaultParamLocation) {
  auto headers = TestHeaderMapImpl{{":path", "/path?access_token=jwt_token"}};
  std::vector<std::unique_ptr<JwtTokenExtractor::Token>> tokens;
  extractor_->Extract(headers, &tokens);
  EXPECT_EQ(tokens.size(), 1);
  EXPECT_EQ(tokens[0]->token(), "jwt_token");

  EXPECT_TRUE(tokens[0]->IsIssuerAllowed("issuer1"));

  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer2"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer3"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer4"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("unknown_issuer"));
}

TEST_F(JwtTokenExtractorTest, TestCustomHeaderToken) {
  auto headers = TestHeaderMapImpl{{"token-header", "jwt_token"}};
  std::vector<std::unique_ptr<JwtTokenExtractor::Token>> tokens;
  extractor_->Extract(headers, &tokens);
  EXPECT_EQ(tokens.size(), 1);

  EXPECT_EQ(tokens[0]->token(), "jwt_token");

  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer1"));
  EXPECT_TRUE(tokens[0]->IsIssuerAllowed("issuer2"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer3"));
  EXPECT_TRUE(tokens[0]->IsIssuerAllowed("issuer4"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("unknown_issuer"));

  // Test token remove
  tokens[0]->Remove(&headers);
  EXPECT_FALSE(headers.get(LowerCaseString("token-header")));
}

TEST_F(JwtTokenExtractorTest, TestCustomParamToken) {
  auto headers = TestHeaderMapImpl{{":path", "/path?token_param=jwt_token"}};
  std::vector<std::unique_ptr<JwtTokenExtractor::Token>> tokens;
  extractor_->Extract(headers, &tokens);
  EXPECT_EQ(tokens.size(), 1);

  EXPECT_EQ(tokens[0]->token(), "jwt_token");

  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer1"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("issuer2"));
  EXPECT_TRUE(tokens[0]->IsIssuerAllowed("issuer3"));
  EXPECT_TRUE(tokens[0]->IsIssuerAllowed("issuer4"));
  EXPECT_FALSE(tokens[0]->IsIssuerAllowed("unknown_issuer"));
}

TEST_F(JwtTokenExtractorTest, TestMultipleTokens) {
  auto headers = TestHeaderMapImpl{{":path", "/path?token_param=param_token"},
                                   {"token-header", "header_token"}};
  std::vector<std::unique_ptr<JwtTokenExtractor::Token>> tokens;
  extractor_->Extract(headers, &tokens);
  EXPECT_EQ(tokens.size(), 1);

  // Header token first.
  EXPECT_EQ(tokens[0]->token(), "header_token");
}

}  // namespace JwtAuth
}  // namespace Http
}  // namespace Envoy
