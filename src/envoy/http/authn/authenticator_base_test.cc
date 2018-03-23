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
#include "src/envoy/http/authn/test_utils.h"
#include "src/istio/authn/context.pb.h"
#include "test/mocks/network/mocks.h"
#include "test/mocks/ssl/mocks.h"
#include "test/test_common/utility.h"

using istio::authn::Payload;
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
  NiceMock<Envoy::Ssl::MockConnection> ssl_{};
  FilterContext filter_context_{&request_headers_, &connection_};
  MockAuthenticatorBase authenticator_{&filter_context_};
};

TEST_F(AuthenticatorBaseTest, ValidateX509OnPlaintextConnection) {
  iaapi::MutualTls mTlsParams;
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                EXPECT_FALSE(payload);
                                EXPECT_FALSE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateX509OnSslConnectionWithNoPeerCert) {
  iaapi::MutualTls mTlsParams;
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(false));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                EXPECT_FALSE(payload);
                                EXPECT_FALSE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateX509OnSslConnectionWithPeerCert) {
  iaapi::MutualTls mTlsParams;
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(true));
  EXPECT_CALL(ssl_, uriSanPeerCertificate()).Times(1).WillOnce(Return("foo"));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                EXPECT_EQ(payload->x509().user(), "foo");
                                EXPECT_TRUE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateX509OnSslConnectionWithPeerSpiffeCert) {
  iaapi::MutualTls mTlsParams;
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(true));
  EXPECT_CALL(ssl_, uriSanPeerCertificate())
      .Times(1)
      .WillOnce(Return("spiffe://foo"));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                EXPECT_EQ(payload->x509().user(), "foo");
                                EXPECT_TRUE(success);
                              });
}

TEST_F(AuthenticatorBaseTest,
       ValidateX509OnSslConnectionWithPeerMalformedSpiffeCert) {
  iaapi::MutualTls mTlsParams;
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(true));
  EXPECT_CALL(ssl_, uriSanPeerCertificate())
      .Times(1)
      .WillOnce(Return("spiffe:foo"));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                EXPECT_EQ(payload->x509().user(), "spiffe:foo");
                                EXPECT_TRUE(success);
                              });
}

// TODO: more tests for Jwt.

}  // namespace
}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
