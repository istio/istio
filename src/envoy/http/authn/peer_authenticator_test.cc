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

#include "src/envoy/http/authn/peer_authenticator.h"
#include "authentication/v1alpha1/policy.pb.h"
#include "common/http/header_map_impl.h"
#include "common/protobuf/protobuf.h"
#include "gmock/gmock.h"
#include "gtest/gtest.h"
#include "src/envoy/http/authn/test_utils.h"
#include "test/mocks/http/mocks.h"
#include "test/test_common/utility.h"

namespace iaapi = istio::authentication::v1alpha1;

using istio::authn::Payload;
using testing::DoAll;
using testing::MockFunction;
using testing::NiceMock;
using testing::Return;
using testing::SetArgPointee;
using testing::StrictMock;
using testing::_;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {
namespace {

class MockPeerAuthenticator : public PeerAuthenticator {
 public:
  MockPeerAuthenticator(FilterContext* filter_context,
                        const DoneCallback& done_callback,
                        const istio::authentication::v1alpha1::Policy& policy)
      : PeerAuthenticator(filter_context, done_callback, policy) {}

  MOCK_CONST_METHOD2(validateX509,
                     void(const iaapi::MutualTls&, const MethodDoneCallback&));
  MOCK_CONST_METHOD2(validateJwt,
                     void(const iaapi::Jwt&, const MethodDoneCallback&));
};

class PeerAuthenticatorTest : public testing::Test {
 public:
  PeerAuthenticatorTest()
      : request_headers_{{":method", "GET"}, {":path", "/"}} {}
  virtual ~PeerAuthenticatorTest() {}

  void createAuthenticator() {
    authenticator_.reset(new StrictMock<MockPeerAuthenticator>(
        &filter_context_, on_done_callback_.AsStdFunction(), policy_));
  }

 protected:
  std::unique_ptr<StrictMock<MockPeerAuthenticator>> authenticator_;
  StrictMock<MockFunction<void(bool)>> on_done_callback_;
  Http::TestHeaderMapImpl request_headers_;
  FilterContext filter_context_{&request_headers_, nullptr};
  iaapi::Policy policy_;

  Payload x509_payload_{TestUtilities::CreateX509Payload("foo")};
  Payload jwt_payload_{TestUtilities::CreateJwtPayload("foo", "istio.io")};
};

TEST_F(PeerAuthenticatorTest, EmptyPolicy) {
  createAuthenticator();
  EXPECT_CALL(on_done_callback_, Call(true)).Times(1);
  authenticator_->run();
  EXPECT_TRUE(TestUtility::protoEqual(TestUtilities::AuthNResultFromString(""),
                                      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, MTlsOnlyPass) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
      peers {
        mtls {
        }
      }
    )",
                                                    &policy_));

  createAuthenticator();
  EXPECT_CALL(*authenticator_, validateX509(_, _))
      .Times(1)
      .WillOnce(testing::InvokeArgument<1>(&x509_payload_, true));
  EXPECT_CALL(on_done_callback_, Call(true)).Times(1);
  authenticator_->run();
  EXPECT_TRUE(TestUtility::protoEqual(
      TestUtilities::AuthNResultFromString(R"(peer_user: "foo")"),
      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, MTlsOnlyFail) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
      peers {
        mtls {
        }
      }
    )",
                                                    &policy_));

  createAuthenticator();
  EXPECT_CALL(*authenticator_, validateX509(_, _))
      .Times(1)
      .WillOnce(testing::InvokeArgument<1>(&x509_payload_, false));
  EXPECT_CALL(on_done_callback_, Call(false)).Times(1);
  authenticator_->run();
  EXPECT_TRUE(TestUtility::protoEqual(TestUtilities::AuthNResultFromString(""),
                                      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, JwtOnlyPass) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
    peers {
      jwt {
        issuer: "abc.xyz"
      }
    }
  )",
                                                    &policy_));

  createAuthenticator();
  EXPECT_CALL(*authenticator_, validateJwt(_, _))
      .Times(1)
      .WillOnce(testing::InvokeArgument<1>(&jwt_payload_, true));
  EXPECT_CALL(on_done_callback_, Call(true)).Times(1);
  authenticator_->run();
  EXPECT_TRUE(TestUtility::protoEqual(
      TestUtilities::AuthNResultFromString(R"(peer_user: "foo")"),
      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, JwtOnlyFail) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
    peers {
      jwt {
        issuer: "abc.xyz"
      }
    }
  )",
                                                    &policy_));

  createAuthenticator();
  EXPECT_CALL(*authenticator_, validateJwt(_, _))
      .Times(1)
      .WillOnce(testing::InvokeArgument<1>(&jwt_payload_, false));
  EXPECT_CALL(on_done_callback_, Call(false)).Times(1);
  authenticator_->run();
  EXPECT_TRUE(TestUtility::protoEqual(TestUtilities::AuthNResultFromString(""),
                                      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, Multiple) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
    peers {
      mtls {}
    }
    peers {
      jwt {
        issuer: "abc.xyz"
      }
    }
    peers {
      jwt {
        issuer: "another"
      }
    }
  )",
                                                    &policy_));

  createAuthenticator();
  EXPECT_CALL(*authenticator_, validateX509(_, _))
      .Times(1)
      .WillOnce(testing::InvokeArgument<1>(nullptr, false));
  EXPECT_CALL(*authenticator_, validateJwt(_, _))
      .Times(1)
      .WillOnce(testing::InvokeArgument<1>(&jwt_payload_, true));
  EXPECT_CALL(on_done_callback_, Call(true)).Times(1);
  authenticator_->run();
  EXPECT_TRUE(TestUtility::protoEqual(
      TestUtilities::AuthNResultFromString(R"(peer_user: "foo")"),
      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, MultipleAllFail) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
    peers {
      mtls {}
    }
    peers {
      jwt {
        issuer: "abc.xyz"
      }
    }
    peers {
      jwt {
        issuer: "another"
      }
    }
  )",
                                                    &policy_));

  createAuthenticator();
  EXPECT_CALL(*authenticator_, validateX509(_, _))
      .Times(1)
      .WillOnce(testing::InvokeArgument<1>(nullptr, false));
  EXPECT_CALL(*authenticator_, validateJwt(_, _))
      .Times(2)
      .WillRepeatedly(testing::InvokeArgument<1>(nullptr, false));
  EXPECT_CALL(on_done_callback_, Call(false)).Times(1);
  authenticator_->run();
  EXPECT_TRUE(TestUtility::protoEqual(TestUtilities::AuthNResultFromString(""),
                                      filter_context_.authenticationResult()));
}

}  // namespace
}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
