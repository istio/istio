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
#include "common/common/base64.h"
#include "common/protobuf/protobuf.h"
#include "envoy/config/filter/http/authn/v2alpha1/config.pb.h"
#include "gmock/gmock.h"
#include "src/envoy/http/authn/test_utils.h"
#include "test/mocks/network/mocks.h"
#include "test/mocks/ssl/mocks.h"

using google::protobuf::util::MessageDifferencer;
using istio::authn::Payload;
using istio::envoy::config::filter::http::authn::v2alpha1::FilterConfig;
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
    R"(
     {
       "iss": "issuer@foo.com",
       "sub": "sub@foo.com",
       "aud": "aud1",
       "non-string-will-be-ignored": 1512754205,
       "some-other-string-claims": "some-claims-kept"
     }
   )";

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
  FilterConfig filter_config_{};
  FilterContext filter_context_{&request_headers_, &connection_,
                                istio::envoy::config::filter::http::authn::
                                    v2alpha1::FilterConfig::default_instance()};

  MockAuthenticatorBase authenticator_{&filter_context_};
};

Http::TestHeaderMapImpl CreateTestHeaderMap(const std::string& header_key,
                                            const std::string& header_value) {
  // The base64 encoding is done through Base64::encode().
  // If the test input has special chars, may need to use the counterpart of
  // Base64UrlDecode().
  std::string value_base64 =
      Base64::encode(header_value.c_str(), header_value.size());
  return Http::TestHeaderMapImpl{{header_key, value_base64}};
}

TEST_F(AuthenticatorBaseTest, ValidateMtlsOnPlaintextConnection) {
  iaapi::MutualTls mTlsParams;
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                // When requiring mTLS, plaintext connection
                                // should fail.
                                EXPECT_FALSE(payload);
                                EXPECT_FALSE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateTlsOnPlaintextConnection) {
  iaapi::MutualTls mTlsParams;
  mTlsParams.set_allow_tls(true);  // allow TLS connection
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                // When requiring TLS, plaintext connection
                                // should fail.
                                EXPECT_FALSE(payload);
                                EXPECT_FALSE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateMtlsOnSslConnectionWithNoPeerCert) {
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

TEST_F(AuthenticatorBaseTest, ValidateTlsOnSslConnectionWithNoPeerCert) {
  iaapi::MutualTls mTlsParams;
  mTlsParams.set_allow_tls(true);  // allow TLS connection
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(false));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                EXPECT_TRUE(payload);
                                // When client certificate is not present on
                                // TLS, authentication should still succeed.
                                EXPECT_TRUE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateMtlsOnSslConnectionWithPeerCert) {
  iaapi::MutualTls mTlsParams;
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(true));
  EXPECT_CALL(ssl_, uriSanPeerCertificate()).Times(1).WillOnce(Return("foo"));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                // When client certificate is present on
                                // mTLS, authenticated attribute should be
                                // extracted.
                                EXPECT_EQ(payload->x509().user(), "foo");
                                EXPECT_TRUE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateTlsOnSslConnectionWithPeerCert) {
  iaapi::MutualTls mTlsParams;
  mTlsParams.set_allow_tls(true);  // allow TLS connection
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(true));
  EXPECT_CALL(ssl_, uriSanPeerCertificate()).Times(1).WillOnce(Return("foo"));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                // When client certificate is present on
                                // TLS, authenticated attribute should be
                                // extracted.
                                EXPECT_EQ(payload->x509().user(), "foo");
                                EXPECT_TRUE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateMtlsOnSslConnectionWithPeerSpiffeCert) {
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
                                // When client certificate is present on
                                // mTLS, authenticated attribute should be
                                // extracted.
                                EXPECT_EQ(payload->x509().user(), "foo");
                                EXPECT_TRUE(success);
                              });
}

TEST_F(AuthenticatorBaseTest, ValidateTlsOnSslConnectionWithPeerSpiffeCert) {
  iaapi::MutualTls mTlsParams;
  mTlsParams.set_allow_tls(true);  // allow TLS connection
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(true));
  EXPECT_CALL(ssl_, uriSanPeerCertificate())
      .Times(1)
      .WillOnce(Return("spiffe://foo"));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                // When client certificate is present on
                                // TLS, authenticated attribute should be
                                // extracted.
                                EXPECT_EQ(payload->x509().user(), "foo");
                                EXPECT_TRUE(success);
                              });
}

TEST_F(AuthenticatorBaseTest,
       ValidateMtlsOnSslConnectionWithPeerMalformedSpiffeCert) {
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
                                // When client certificate is present on mTLS
                                // and the spiffe subject format is wrong
                                // ("spiffe:foo" instead of "spiffe://foo"), the
                                // user attribute should be extracted.
                                EXPECT_EQ(payload->x509().user(), "spiffe:foo");
                                EXPECT_TRUE(success);
                              });
}

TEST_F(AuthenticatorBaseTest,
       ValidateTlsOnSslConnectionWithPeerMalformedSpiffeCert) {
  iaapi::MutualTls mTlsParams;
  mTlsParams.set_allow_tls(true);  // allow TLS connection
  EXPECT_CALL(Const(connection_), ssl()).WillRepeatedly(Return(&ssl_));
  EXPECT_CALL(Const(ssl_), peerCertificatePresented())
      .Times(1)
      .WillOnce(Return(true));
  EXPECT_CALL(ssl_, uriSanPeerCertificate())
      .Times(1)
      .WillOnce(Return("spiffe:foo"));
  authenticator_.validateX509(mTlsParams,
                              [](const Payload* payload, bool success) {
                                // When client certificate is present on TLS and
                                // the spiffe subject format is wrong
                                // ("spiffe:foo" instead of "spiffe://foo"), the
                                // user attribute should be extracted.
                                EXPECT_EQ(payload->x509().user(), "spiffe:foo");
                                EXPECT_TRUE(success);
                              });
}

// TODO: more tests for Jwt.
TEST_F(AuthenticatorBaseTest, ValidateJwtWithNoIstioAuthnConfig) {
  iaapi::Jwt jwt;
  jwt.set_issuer("issuer@foo.com");
  // authenticator_ has empty Istio authn config
  authenticator_.validateJwt(jwt, [](const Payload* payload, bool success) {
    // When there is empty Istio authn config, validateJwt() should return
    // nullptr and failure.
    EXPECT_TRUE(payload == nullptr);
    EXPECT_FALSE(success);
  });
}

TEST_F(AuthenticatorBaseTest, ValidateJwtWithNoIssuer) {
  // no issuer in jwt
  iaapi::Jwt jwt;
  google::protobuf::util::JsonParseOptions options;
  FilterConfig filter_config;
  JsonStringToMessage(
      R"({
              "jwt_output_payload_locations":
              {
                "issuer@foo.com": "sec-istio-auth-userinfo"
              }
           }
        )",
      &filter_config, options);
  Http::TestHeaderMapImpl empty_request_headers{};
  FilterContext filter_context{&empty_request_headers, &connection_,
                               filter_config};
  MockAuthenticatorBase authenticator{&filter_context};
  authenticator.validateJwt(jwt, [](const Payload* payload, bool success) {
    // When there is no issuer in the JWT config, validateJwt() should return
    // nullptr and failure.
    EXPECT_TRUE(payload == nullptr);
    EXPECT_FALSE(success);
  });
}

TEST_F(AuthenticatorBaseTest, ValidateJwtWithEmptyJwtOutputPayloadLocations) {
  iaapi::Jwt jwt;
  jwt.set_issuer("issuer@foo.com");
  Http::TestHeaderMapImpl request_headers_with_jwt = CreateTestHeaderMap(
      kSecIstioAuthUserInfoHeaderKey, kSecIstioAuthUserinfoHeaderValue);
  google::protobuf::util::JsonParseOptions options;
  FilterConfig filter_config;
  JsonStringToMessage(
      R"({
              "jwt_output_payload_locations":
              {
              }
           }
        )",
      &filter_config, options);
  FilterContext filter_context{&request_headers_with_jwt, &connection_,
                               filter_config};
  MockAuthenticatorBase authenticator{&filter_context};
  // authenticator has empty jwt_output_payload_locations in Istio authn config
  authenticator.validateJwt(jwt, [](const Payload* payload, bool success) {
    // When there is no matching jwt_output_payload_locations for the issuer in
    // the Istio authn config, validateJwt() should return nullptr and failure.
    EXPECT_TRUE(payload == nullptr);
    EXPECT_FALSE(success);
  });
}

TEST_F(AuthenticatorBaseTest, ValidateJwtWithNoJwtInHeader) {
  iaapi::Jwt jwt;
  jwt.set_issuer("issuer@foo.com");
  google::protobuf::util::JsonParseOptions options;
  FilterConfig filter_config;
  JsonStringToMessage(
      R"({
              "jwt_output_payload_locations":
              {
                "issuer@foo.com": "sec-istio-auth-jwt-output"
              }
           }
        )",
      &filter_config, options);
  Http::TestHeaderMapImpl empty_request_headers{};
  FilterContext filter_context{&empty_request_headers, &connection_,
                               filter_config};
  MockAuthenticatorBase authenticator{&filter_context};
  authenticator.validateJwt(jwt, [](const Payload* payload, bool success) {
    // When there is no JWT in the HTTP header, validateJwt() should return
    // nullptr and failure.
    EXPECT_TRUE(payload == nullptr);
    EXPECT_FALSE(success);
  });
}

TEST_F(AuthenticatorBaseTest, ValidateJwtWithJwtInHeader) {
  iaapi::Jwt jwt;
  jwt.set_issuer("issuer@foo.com");
  Http::TestHeaderMapImpl request_headers_with_jwt = CreateTestHeaderMap(
      "sec-istio-auth-jwt-output", kSecIstioAuthUserinfoHeaderValue);
  google::protobuf::util::JsonParseOptions options;
  FilterConfig filter_config;
  JsonStringToMessage(
      R"({
              "jwt_output_payload_locations":
              {
                "issuer@foo.com": "sec-istio-auth-jwt-output"
              }
           }
        )",
      &filter_config, options);
  FilterContext filter_context{&request_headers_with_jwt, &connection_,
                               filter_config};
  MockAuthenticatorBase authenticator{&filter_context};
  Payload expected_payload;
  JsonStringToMessage(
      R"({
             "jwt": {
               "user": "issuer@foo.com/sub@foo.com",
               "audiences": ["aud1"],
               "presenter": "",
               "claims": {
                 "aud": "aud1",
                 "iss": "issuer@foo.com",
                 "sub": "sub@foo.com",
                 "some-other-string-claims": "some-claims-kept"
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
