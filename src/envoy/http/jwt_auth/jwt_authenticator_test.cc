/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#include "src/envoy/http/jwt_auth/jwt_authenticator.h"
#include "common/http/message_impl.h"
#include "common/json/json_loader.h"
#include "gtest/gtest.h"
#include "test/mocks/upstream/mocks.h"
#include "test/test_common/utility.h"

using ::testing::_;
using ::testing::Invoke;
using ::testing::NiceMock;

namespace Envoy {
namespace Http {
namespace JwtAuth {
namespace {

// A good public key
const std::string kPublicKey =
    "{\"keys\": [{\"kty\": \"RSA\",\"alg\": \"RS256\",\"use\": "
    "\"sig\",\"kid\": \"62a93512c9ee4c7f8067b5a216dade2763d32a47\",\"n\": "
    "\"0YWnm_eplO9BFtXszMRQNL5UtZ8HJdTH2jK7vjs4XdLkPW7YBkkm_"
    "2xNgcaVpkW0VT2l4mU3KftR-6s3Oa5Rnz5BrWEUkCTVVolR7VYksfqIB2I_"
    "x5yZHdOiomMTcm3DheUUCgbJRv5OKRnNqszA4xHn3tA3Ry8VO3X7BgKZYAUh9fyZTFLlkeAh"
    "0-"
    "bLK5zvqCmKW5QgDIXSxUTJxPjZCgfx1vmAfGqaJb-"
    "nvmrORXQ6L284c73DUL7mnt6wj3H6tVqPKA27j56N0TB1Hfx4ja6Slr8S4EB3F1luYhATa1P"
    "KU"
    "SH8mYDW11HolzZmTQpRoLV8ZoHbHEaTfqX_aYahIw\",\"e\": \"AQAB\"},{\"kty\": "
    "\"RSA\",\"alg\": \"RS256\",\"use\": \"sig\",\"kid\": "
    "\"b3319a147514df7ee5e4bcdee51350cc890cc89e\",\"n\": "
    "\"qDi7Tx4DhNvPQsl1ofxxc2ePQFcs-L0mXYo6TGS64CY_"
    "2WmOtvYlcLNZjhuddZVV2X88m0MfwaSA16wE-"
    "RiKM9hqo5EY8BPXj57CMiYAyiHuQPp1yayjMgoE1P2jvp4eqF-"
    "BTillGJt5W5RuXti9uqfMtCQdagB8EC3MNRuU_KdeLgBy3lS3oo4LOYd-"
    "74kRBVZbk2wnmmb7IhP9OoLc1-7-9qU1uhpDxmE6JwBau0mDSwMnYDS4G_ML17dC-"
    "ZDtLd1i24STUw39KH0pcSdfFbL2NtEZdNeam1DDdk0iUtJSPZliUHJBI_pj8M-2Mn_"
    "oA8jBuI8YKwBqYkZCN1I95Q\",\"e\": \"AQAB\"}]}";

// A good JSON config.
const char kExampleConfig[] = R"(
{
   "jwts": [
      {
         "issuer": "https://example.com",
         "audiences": [
            "example_service"
          ],
         "jwks_uri": "https://pubkey_server/pubkey_path",
         "jwks_uri_envoy_cluster": "pubkey_cluster",
         "public_key_cache_duration": {
            "seconds": 600
         }
      }
   ]
}
)";

// A JSON config for "other_issuer"
const char kOtherIssuerConfig[] = R"(
{
   "jwts": [
      {
         "issuer": "other_issuer",
         "jwks_uri": "https://pubkey_server/pubkey_path",
         "jwks_uri_envoy_cluster": "pubkey_cluster"
      }
   ]
}
)";

// expired token
const std::string kExpiredToken =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9."
    "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
    "ImV4cCI6MTUwMTI4MTA1OH0.XYPg6VPrq-H1Kl-kgmAfGFomVpnmdZLIAo0g6dhJb2Be_"
    "koZ2T76xg5_Lr828hsLKxUfzwNxl5-k1cdz_kAst6vei0hdnOYqRQ8EhkZS_"
    "5Y2vWMrzGHw7AUPKCQvSnNqJG5HV8YdeOfpsLhQTd-"
    "tG61q39FWzJ5Ra5lkxWhcrVDQFtVy7KQrbm2dxhNEHAR2v6xXP21p1T5xFBdmGZbHFiH63N9"
    "dwdRgWjkvPVTUqxrZil7PSM2zg_GTBETp_"
    "qS7Wwf8C0V9o2KZu0KDV0j0c9nZPWTv3IMlaGZAtQgJUeyemzRDtf4g2yG3xBZrLm3AzDUj_"
    "EX_pmQAHA5ZjPVCAw";

// A token with aud as invalid_service
// Payload:
// {"iss":"https://example.com","sub":"test@example.com","aud":"invalid_service","exp":2001001001}
const std::string kInvalidAudToken =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9."
    "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
    "ImF1ZCI6ImludmFsaWRfc2VydmljZSIsImV4cCI6MjAwMTAwMTAwMX0."
    "gEWnuqtEdVzC94lVbuClaVLoxs-w-_uKJRbAYwAKRulE-"
    "ZhxG9VtKCd8i90xEuk9txB3tT8VGjdZKs5Hf5LjF4ebobV3M9ya6mZvq1MdcUHYiUtQhJe3M"
    "t_2sxRmogK-QZ7HcuA9hpFO4HHVypnMDr4WHgxx2op1vhKU7NDlL-"
    "38Dpf6uKEevxi0Xpids9pSST4YEQjReTXJDJECT5dhk8ZQ_lcS-pujgn7kiY99bTf6j4U-"
    "ajIcWwtQtogYx4bcmHBUvEjcYOC86TRrnArZSk1mnO7OGq4KrSrqhXnvqDmc14LfldyWqEks"
    "X5FkM94prXPK0iN-pPVhRjNZ4xvR-w";

// Payload:
// {"iss":"https://example.com","sub":"test@example.com","aud":"example_service","exp":2001001001}
const std::string kGoodToken =
    "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9."
    "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
    "ImF1ZCI6ImV4YW1wbGVfc2VydmljZSIsImV4cCI6MjAwMTAwMTAwMX0."
    "n45uWZfIBZwCIPiL0K8Ca3tmm-ZlsDrC79_"
    "vXCspPwk5oxdSn983tuC9GfVWKXWUMHe11DsB02b19Ow-"
    "fmoEzooTFn65Ml7G34nW07amyM6lETiMhNzyiunctplOr6xKKJHmzTUhfTirvDeG-q9n24-"
    "8lH7GP8GgHvDlgSM9OY7TGp81bRcnZBmxim_UzHoYO3_"
    "c8OP4ZX3xG5PfihVk5G0g6wcHrO70w0_64JgkKRCrLHMJSrhIgp9NHel_"
    "CNOnL0AjQKe9IGblJrMuouqYYS0zEWwmOVUWUSxQkoLpldQUVefcfjQeGjz8IlvktRa77FYe"
    "xfP590ACPyXrivtsxg";

}  // namespace

class MockJwtAuthenticatorCallbacks : public JwtAuthenticator::Callbacks {
 public:
  MOCK_METHOD1(onDone, void(const Status& status));
};

class JwtAuthenticatorTest : public ::testing::Test {
 public:
  void SetUp() { SetupConfig(kExampleConfig); }

  void SetupConfig(const std::string& json_str) {
    google::protobuf::util::Status status =
        ::google::protobuf::util::JsonStringToMessage(json_str, &config_);
    ASSERT_TRUE(status.ok());
    store_.reset(new JwtAuthStore(config_));
    auth_.reset(new JwtAuthenticator(mock_cm_, *store_));
  }

  Config::AuthFilterConfig config_;
  std::unique_ptr<JwtAuthStore> store_;
  std::unique_ptr<JwtAuthenticator> auth_;
  NiceMock<Upstream::MockClusterManager> mock_cm_;
  MockJwtAuthenticatorCallbacks mock_cb_;
};

TEST_F(JwtAuthenticatorTest, TestOkJWT) {
  NiceMock<Http::MockAsyncClient> async_client;
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_))
      .WillOnce(Invoke([&](const std::string& cluster) -> Http::AsyncClient& {
        EXPECT_EQ(cluster, "pubkey_cluster");
        return async_client;
      }));

  MockAsyncClientRequest request(&async_client);
  AsyncClient::Callbacks* callbacks;
  EXPECT_CALL(async_client, send_(_, _, _))
      .WillOnce(Invoke([&](MessagePtr& message, AsyncClient::Callbacks& cb,
                           const Optional<std::chrono::milliseconds>&)
                           -> AsyncClient::Request* {
                             EXPECT_EQ((TestHeaderMapImpl{
                                           {":method", "GET"},
                                           {":path", "/pubkey_path"},
                                           {":authority", "pubkey_server"},
                                       }),
                                       message->headers());
                             callbacks = &cb;
                             return &request;
                           }));

  // Test OK pubkey and its cache
  for (int i = 0; i < 10; i++) {
    auto headers = TestHeaderMapImpl{{"Authorization", "Bearer " + kGoodToken}};

    MockJwtAuthenticatorCallbacks mock_cb;
    EXPECT_CALL(mock_cb, onDone(_))
        .WillOnce(Invoke(
            [](const Status& status) { ASSERT_EQ(status, Status::OK); }));

    auth_->Verify(headers, &mock_cb);

    if (i == 0) {
      Http::MessagePtr response_message(new ResponseMessageImpl(
          HeaderMapPtr{new TestHeaderMapImpl{{":status", "200"}}}));
      response_message->body().reset(new Buffer::OwnedImpl(kPublicKey));
      callbacks->onSuccess(std::move(response_message));
    }

    EXPECT_EQ(headers.get_("sec-istio-auth-userinfo"),
              "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVz"
              "dEBleGFtcGxlLmNvbSIsImF1ZCI6ImV4YW1wbGVfc2VydmljZSIs"
              "ImV4cCI6MjAwMTAwMTAwMX0");
  }
}

TEST_F(JwtAuthenticatorTest, TestMissedJWT) {
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_)).Times(0);
  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke(
          [](const Status& status) { ASSERT_EQ(status, Status::JWT_MISSED); }));

  // Empty headers.
  auto headers = TestHeaderMapImpl{};
  auth_->Verify(headers, &mock_cb_);
}

TEST_F(JwtAuthenticatorTest, TestInvalidJWT) {
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_)).Times(0);
  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke([](const Status& status) {
        ASSERT_EQ(status, Status::JWT_BAD_FORMAT);
      }));

  std::string token = "invalidToken";
  auto headers = TestHeaderMapImpl{{"Authorization", "Bearer " + token}};
  auth_->Verify(headers, &mock_cb_);
}

TEST_F(JwtAuthenticatorTest, TestInvalidPrefix) {
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_)).Times(0);
  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke([](const Status& status) {
        ASSERT_EQ(status, Status::BEARER_PREFIX_MISMATCH);
      }));

  auto headers = TestHeaderMapImpl{{"Authorization", "Bearer-invalid"}};
  auth_->Verify(headers, &mock_cb_);
}

TEST_F(JwtAuthenticatorTest, TestExpiredJWT) {
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_)).Times(0);
  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke([](const Status& status) {
        ASSERT_EQ(status, Status::JWT_EXPIRED);
      }));

  auto headers =
      TestHeaderMapImpl{{"Authorization", "Bearer " + kExpiredToken}};
  auth_->Verify(headers, &mock_cb_);
}

TEST_F(JwtAuthenticatorTest, TestNonMatchAudJWT) {
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_)).Times(0);
  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke([](const Status& status) {
        ASSERT_EQ(status, Status::AUDIENCE_NOT_ALLOWED);
      }));

  auto headers =
      TestHeaderMapImpl{{"Authorization", "Bearer " + kInvalidAudToken}};
  auth_->Verify(headers, &mock_cb_);
}

TEST_F(JwtAuthenticatorTest, TestWrongCluster) {
  // Get returns nullptr
  EXPECT_CALL(mock_cm_, get(_))
      .WillOnce(Invoke(
          [](const std::string& cluster) -> Upstream::ThreadLocalCluster* {
            EXPECT_EQ(cluster, "pubkey_cluster");
            return nullptr;
          }));

  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_)).Times(0);
  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke([](const Status& status) {
        ASSERT_EQ(status, Status::FAILED_FETCH_PUBKEY);
      }));

  auto headers = TestHeaderMapImpl{{"Authorization", "Bearer " + kGoodToken}};
  auth_->Verify(headers, &mock_cb_);
}

TEST_F(JwtAuthenticatorTest, TestIssuerNotFound) {
  // Create a config with an other issuer.
  SetupConfig(kOtherIssuerConfig);

  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_)).Times(0);
  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke([](const Status& status) {
        ASSERT_EQ(status, Status::JWT_UNKNOWN_ISSUER);
      }));

  auto headers = TestHeaderMapImpl{{"Authorization", "Bearer " + kGoodToken}};
  auth_->Verify(headers, &mock_cb_);
}

TEST_F(JwtAuthenticatorTest, TestPubkeyFetchFail) {
  NiceMock<Http::MockAsyncClient> async_client;
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_))
      .WillOnce(Invoke([&](const std::string& cluster) -> Http::AsyncClient& {
        EXPECT_EQ(cluster, "pubkey_cluster");
        return async_client;
      }));

  MockAsyncClientRequest request(&async_client);
  AsyncClient::Callbacks* callbacks;
  EXPECT_CALL(async_client, send_(_, _, _))
      .WillOnce(Invoke([&](MessagePtr& message, AsyncClient::Callbacks& cb,
                           const Optional<std::chrono::milliseconds>&)
                           -> AsyncClient::Request* {
                             EXPECT_EQ((TestHeaderMapImpl{
                                           {":method", "GET"},
                                           {":path", "/pubkey_path"},
                                           {":authority", "pubkey_server"},
                                       }),
                                       message->headers());
                             callbacks = &cb;
                             return &request;
                           }));

  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke([](const Status& status) {
        ASSERT_EQ(status, Status::FAILED_FETCH_PUBKEY);
      }));

  auto headers = TestHeaderMapImpl{{"Authorization", "Bearer " + kGoodToken}};
  auth_->Verify(headers, &mock_cb_);

  Http::MessagePtr response_message(new ResponseMessageImpl(
      HeaderMapPtr{new TestHeaderMapImpl{{":status", "401"}}}));
  callbacks->onSuccess(std::move(response_message));
}

TEST_F(JwtAuthenticatorTest, TestInvalidPubkey) {
  NiceMock<Http::MockAsyncClient> async_client;
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_))
      .WillOnce(Invoke([&](const std::string& cluster) -> Http::AsyncClient& {
        EXPECT_EQ(cluster, "pubkey_cluster");
        return async_client;
      }));

  MockAsyncClientRequest request(&async_client);
  AsyncClient::Callbacks* callbacks;
  EXPECT_CALL(async_client, send_(_, _, _))
      .WillOnce(Invoke([&](MessagePtr& message, AsyncClient::Callbacks& cb,
                           const Optional<std::chrono::milliseconds>&)
                           -> AsyncClient::Request* {
                             EXPECT_EQ((TestHeaderMapImpl{
                                           {":method", "GET"},
                                           {":path", "/pubkey_path"},
                                           {":authority", "pubkey_server"},
                                       }),
                                       message->headers());
                             callbacks = &cb;
                             return &request;
                           }));

  EXPECT_CALL(mock_cb_, onDone(_))
      .WillOnce(Invoke([](const Status& status) {
        ASSERT_EQ(status, Status::JWK_PARSE_ERROR);
      }));

  auto headers = TestHeaderMapImpl{{"Authorization", "Bearer " + kGoodToken}};
  auth_->Verify(headers, &mock_cb_);

  Http::MessagePtr response_message(new ResponseMessageImpl(
      HeaderMapPtr{new TestHeaderMapImpl{{":status", "200"}}}));
  response_message->body().reset(new Buffer::OwnedImpl("invalid publik key"));
  callbacks->onSuccess(std::move(response_message));
}

TEST_F(JwtAuthenticatorTest, TestOnDestroy) {
  NiceMock<Http::MockAsyncClient> async_client;
  EXPECT_CALL(mock_cm_, httpAsyncClientForCluster(_))
      .WillOnce(Invoke([&](const std::string& cluster) -> Http::AsyncClient& {
        EXPECT_EQ(cluster, "pubkey_cluster");
        return async_client;
      }));

  MockAsyncClientRequest request(&async_client);
  AsyncClient::Callbacks* callbacks;
  EXPECT_CALL(async_client, send_(_, _, _))
      .WillOnce(Invoke([&](MessagePtr& message, AsyncClient::Callbacks& cb,
                           const Optional<std::chrono::milliseconds>&)
                           -> AsyncClient::Request* {
                             EXPECT_EQ((TestHeaderMapImpl{
                                           {":method", "GET"},
                                           {":path", "/pubkey_path"},
                                           {":authority", "pubkey_server"},
                                       }),
                                       message->headers());
                             callbacks = &cb;
                             return &request;
                           }));

  // Cancel is called once.
  EXPECT_CALL(request, cancel()).Times(1);

  // onDone() should not be called.
  EXPECT_CALL(mock_cb_, onDone(_)).Times(0);

  auto headers = TestHeaderMapImpl{{"Authorization", "Bearer " + kGoodToken}};
  auth_->Verify(headers, &mock_cb_);

  // Destroy the authenticating process.
  auth_->onDestroy();
}

}  // namespace JwtAuth
}  // namespace Http
}  // namespace Envoy
