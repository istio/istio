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
#include "common/protobuf/protobuf.h"
#include "gmock/gmock.h"
#include "src/envoy/http/authn/test_utils.h"
#include "test/mocks/network/mocks.h"
#include "test/mocks/ssl/mocks.h"

using google::protobuf::util::MessageDifferencer;
using istio::authn::Payload;
using testing::NiceMock;
using testing::Return;

namespace iaapi = istio::authentication::v1alpha1;

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {
namespace {

const std::string kSecIstioAuthUserInfoHeaderKey = "sec-istio-auth-userinfo";
const std::string kSecIstioAuthUserinfoHeaderValue =
    "eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OH"
    "Z2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJzdWIiOiI2Mjg2NDU3"
    "NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLm"
    "dzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJib29rc3RvcmUtZXNwLWVjaG8uY2xv"
    "dWRlbmRwb2ludHNhcGlzLmNvbSIsImlhdCI6MTUxMjc1NDIwNSwiZXhwIjo1MTEyNz"
    "U0MjA1fQ==";

class MockAuthenticatorBase : public AuthenticatorBase {
 public:
  MockAuthenticatorBase(FilterContext* filter_context)
      : AuthenticatorBase(filter_context, [](bool) {}) {}
  MOCK_METHOD0(run, void());
};

class AuthenticatorBaseTest : public testing::Test,
                              public Logger::Loggable<Logger::Id::filter> {
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
TEST_F(AuthenticatorBaseTest, ValidateJwtWithNoJwtInHeader) {
  iaapi::Jwt jwt;
  authenticator_.validateJwt(jwt, [](const Payload* payload, bool success) {
    // When there is no JWT in the HTTP header, validateJwt() should return
    // nullptr and failure.
    EXPECT_TRUE(payload == nullptr);
    EXPECT_FALSE(success);
  });
}

TEST_F(AuthenticatorBaseTest, ValidateJwtWithJwtInHeader) {
  iaapi::Jwt jwt;
  Http::TestHeaderMapImpl request_headers_with_jwt{
      {kSecIstioAuthUserInfoHeaderKey, kSecIstioAuthUserinfoHeaderValue}};
  FilterContext filter_context{&request_headers_with_jwt, &connection_};
  MockAuthenticatorBase authenticator{&filter_context};
  Payload expected_payload;
  google::protobuf::util::JsonParseOptions options;
  JsonStringToMessage(
      R"({
             "jwt": {
               "user": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com/628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
               "audiences": ["bookstore-esp-echo.cloudendpointsapis.com"],
               "presenter": "",
               "claims": {
                 "iss": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
                 "sub": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com"
               }
             }
           }
        )",
      &expected_payload, options);

  authenticator.validateJwt(
      jwt, [&expected_payload](const Payload* payload, bool success) {
        // When there is a verified JWT in the HTTP header, validateJwt()
        // should return non-nullptr and success.
        EXPECT_TRUE(payload != nullptr);
        EXPECT_TRUE(success);
        // Note: TestUtility::protoEqual() uses SerializeAsString() and the
        // output is non-deterministic.  Thus, MessageDifferencer::Equals() is
        // used.
        EXPECT_TRUE(MessageDifferencer::Equals(expected_payload, *payload));
      });
}

}  // namespace
}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
