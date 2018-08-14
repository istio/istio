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
#include "common/protobuf/protobuf.h"
#include "envoy/api/v2/core/base.pb.h"
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
                        const istio::authentication::v1alpha1::Policy& policy)
      : PeerAuthenticator(filter_context, policy) {}

  MOCK_CONST_METHOD2(validateX509, bool(const iaapi::MutualTls&, Payload*));
  MOCK_METHOD2(validateJwt, bool(const iaapi::Jwt&, Payload*));
};

class PeerAuthenticatorTest : public testing::Test {
 public:
  PeerAuthenticatorTest() {}
  virtual ~PeerAuthenticatorTest() {}

  void createAuthenticator() {
    authenticator_.reset(
        new StrictMock<MockPeerAuthenticator>(&filter_context_, policy_));
  }

  void SetUp() override { payload_ = new Payload(); }

  void TearDown() override { delete (payload_); }

 protected:
  std::unique_ptr<StrictMock<MockPeerAuthenticator>> authenticator_;
  FilterContext filter_context_{
      envoy::api::v2::core::Metadata::default_instance(), nullptr,
      istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig::
          default_instance()};

  iaapi::Policy policy_;
  Payload* payload_;

  Payload x509_payload_{TestUtilities::CreateX509Payload("foo")};
  Payload jwt_payload_{TestUtilities::CreateJwtPayload("foo", "istio.io")};
  Payload jwt_extra_payload_{
      TestUtilities::CreateJwtPayload("bar", "istio.io")};
};

TEST_F(PeerAuthenticatorTest, EmptyPolicy) {
  createAuthenticator();
  authenticator_->run(payload_);
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
      .WillOnce(DoAll(SetArgPointee<1>(x509_payload_), Return(true)));

  authenticator_->run(payload_);
  EXPECT_TRUE(TestUtility::protoEqual(
      TestUtilities::AuthNResultFromString(R"(peer_user: "foo")"),
      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, TlsOnlyPass) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
      peers {
        mtls {
          allow_tls: true
        }
      }
    )",
                                                    &policy_));

  createAuthenticator();
  EXPECT_CALL(*authenticator_, validateX509(_, _))
      .Times(1)
      .WillOnce(DoAll(SetArgPointee<1>(x509_payload_), Return(true)));

  authenticator_->run(payload_);
  // When client certificate is present on TLS, authenticated attribute
  // should be extracted.
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
      .WillOnce(DoAll(SetArgPointee<1>(x509_payload_), Return(false)));
  authenticator_->run(payload_);
  EXPECT_TRUE(TestUtility::protoEqual(TestUtilities::AuthNResultFromString(""),
                                      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, TlsOnlyFail) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
      peers {
        mtls {
          allow_tls: true
        }
      }
    )",
                                                    &policy_));

  createAuthenticator();
  EXPECT_CALL(*authenticator_, validateX509(_, _))
      .Times(1)
      .WillOnce(DoAll(SetArgPointee<1>(x509_payload_), Return(false)));

  authenticator_->run(payload_);
  // When TLS authentication failse, the authenticated attribute should be
  // empty.
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
      .WillOnce(DoAll(SetArgPointee<1>(x509_payload_), Return(true)));
  authenticator_->run(payload_);
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
      .WillOnce(DoAll(SetArgPointee<1>(x509_payload_), Return(false)));
  authenticator_->run(payload_);
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
      .WillOnce(DoAll(SetArgPointee<1>(jwt_extra_payload_), Return(false)));
  EXPECT_CALL(*authenticator_, validateJwt(_, _))
      .Times(1)
      .WillOnce(DoAll(SetArgPointee<1>(jwt_payload_), Return(true)));

  authenticator_->run(payload_);
  EXPECT_TRUE(TestUtility::protoEqual(
      TestUtilities::AuthNResultFromString(R"(peer_user: "foo")"),
      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, TlsFailAndJwtSucceed) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
    peers {
      mtls { allow_tls: true }
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
      .WillOnce(DoAll(SetArgPointee<1>(jwt_extra_payload_), Return(false)));
  EXPECT_CALL(*authenticator_, validateJwt(_, _))
      .Times(1)
      .WillOnce(DoAll(SetArgPointee<1>(jwt_payload_), Return(true)));
  authenticator_->run(payload_);
  // validateX509 fail and validateJwt succeeds,
  // result should be "foo", as expected as in jwt_payload.
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
      .WillOnce(DoAll(SetArgPointee<1>(jwt_extra_payload_), Return(false)));
  EXPECT_CALL(*authenticator_, validateJwt(_, _))
      .Times(2)
      .WillRepeatedly(
          DoAll(SetArgPointee<1>(jwt_extra_payload_), Return(false)));
  authenticator_->run(payload_);
  EXPECT_TRUE(TestUtility::protoEqual(TestUtilities::AuthNResultFromString(""),
                                      filter_context_.authenticationResult()));
}

TEST_F(PeerAuthenticatorTest, TlsFailJwtFail) {
  ASSERT_TRUE(Protobuf::TextFormat::ParseFromString(R"(
    peers {
      mtls { allow_tls: true }
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
      .WillOnce(DoAll(SetArgPointee<1>(jwt_extra_payload_), Return(false)));
  EXPECT_CALL(*authenticator_, validateJwt(_, _))
      .Times(2)
      .WillRepeatedly(
          DoAll(SetArgPointee<1>(jwt_extra_payload_), Return(false)));
  authenticator_->run(payload_);
  // validateX509 and validateJwt fail, result should be empty.
  EXPECT_TRUE(TestUtility::protoEqual(TestUtilities::AuthNResultFromString(""),
                                      filter_context_.authenticationResult()));
}

}  // namespace
}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
