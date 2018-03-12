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

#include "src/envoy/http/authn/authenticator_base.h"
#include "common/http/header_map_impl.h"
#include "common/protobuf/protobuf.h"
#include "gmock/gmock.h"
#include "gtest/gtest.h"
#include "src/envoy/http/authn/context.pb.h"
#include "src/envoy/http/authn/test_utils.h"
#include "test/mocks/network/mocks.h"
#include "test/test_common/utility.h"

using testing::NiceMock;
using testing::Return;

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {
namespace {

class MockAuthenticatorBase : public AuthenticatorBase {
 public:
  MockAuthenticatorBase(FilterContext* filter_context)
      : AuthenticatorBase(filter_context, [](bool) {}) {}
  MOCK_METHOD0(run, void());
};

class AuthenticatorBaseTest : public testing::Test {
 public:
  virtual ~AuthenticatorBaseTest() {}

  Http::TestHeaderMapImpl request_headers_{};
  NiceMock<Envoy::Network::MockConnection> connection_{};
  FilterContext filter_context_{&request_headers_, &connection_};
  MockAuthenticatorBase authenticator_{&filter_context_};
};

TEST_F(AuthenticatorBaseTest, ValidateX509) {
  iaapi::MutualTls mTlsParams;
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                EXPECT_FALSE(payload);
                                EXPECT_FALSE(success);
                              });
}

// TODO: more tests for other cases of x509 and Jwt.

TEST(FindCredentialRuleTest, EmptyPolicy) {
  iaapi::Policy policy;
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString("", &policy));
  EXPECT_TRUE(TestUtility::protoEqual(iaapi::CredentialRule::default_instance(),
                                      findCredentialRuleOrDefault(policy, "")));
  EXPECT_TRUE(
      TestUtility::protoEqual(iaapi::CredentialRule::default_instance(),
                              findCredentialRuleOrDefault(policy, "foo")));
  // Also make sure the default rule USE_PEER binding (i.e USE_PEER should
  // be the first entry in the Binding enum)
  EXPECT_EQ(iaapi::CredentialRule::USE_PEER,
            iaapi::CredentialRule::default_instance().binding());
}

TEST(FindCredentialRuleTest, WithMatchingPeer) {
  iaapi::Policy policy;
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(
      R"(credential_rules {
           binding: USE_PEER
           matching_peers: "foo"
           matching_peers: "bar"
         }
         credential_rules {
           binding: USE_ORIGIN
           origins: {
             jwt: {
               issuer: "abc"
             }
           }
           matching_peers: "dead"
         }
      )",
      &policy));
  EXPECT_TRUE(TestUtility::protoEqual(
      policy.credential_rules(0), findCredentialRuleOrDefault(policy, "foo")));
  EXPECT_TRUE(TestUtility::protoEqual(
      policy.credential_rules(0), findCredentialRuleOrDefault(policy, "bar")));
  EXPECT_TRUE(TestUtility::protoEqual(
      policy.credential_rules(1), findCredentialRuleOrDefault(policy, "dead")));

  // No matches, return default.
  EXPECT_TRUE(
      TestUtility::protoEqual(iaapi::CredentialRule::default_instance(),
                              findCredentialRuleOrDefault(policy, "beef")));
  // case sensitive, FOO != foo.
  EXPECT_TRUE(
      TestUtility::protoEqual(iaapi::CredentialRule::default_instance(),
                              findCredentialRuleOrDefault(policy, "FOO")));
  EXPECT_TRUE(TestUtility::protoEqual(iaapi::CredentialRule::default_instance(),
                                      findCredentialRuleOrDefault(policy, "")));
}

TEST(FindCredentialRuleTest, WithOutMatchingPeer) {
  iaapi::Policy policy;
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(
      R"(credential_rules {
           binding: USE_PEER
           matching_peers: "foo"
           matching_peers: "bar"
         }
         credential_rules {
           binding: USE_ORIGIN
           origins: {
             jwt: {
               issuer: "xyz"
             }
           }
         }
      )",
      &policy));

  EXPECT_TRUE(TestUtility::protoEqual(
      policy.credential_rules(0), findCredentialRuleOrDefault(policy, "foo")));

  // Rule 1 without matching criteria will match anything.
  EXPECT_TRUE(TestUtility::protoEqual(
      policy.credential_rules(1), findCredentialRuleOrDefault(policy, "beef")));
  EXPECT_TRUE(TestUtility::protoEqual(
      policy.credential_rules(1), findCredentialRuleOrDefault(policy, "FOO")));
  EXPECT_TRUE(TestUtility::protoEqual(policy.credential_rules(1),
                                      findCredentialRuleOrDefault(policy, "")));
}

}  // namespace
}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
