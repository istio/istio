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

#include "test/integration/http_integration.h"
#include "test/integration/utility.h"

namespace Envoy {

// Base class JWT filter integration tests.
class JwtVerificationFilterIntegrationTest
    : public HttpIntegrationTest,
      public testing::TestWithParam<Network::Address::IpVersion> {
 public:
  JwtVerificationFilterIntegrationTest()
      : HttpIntegrationTest(Http::CodecClient::Type::HTTP1, GetParam()) {}
  virtual ~JwtVerificationFilterIntegrationTest() {}
  /**
   * Initializer for an individual integration test.
   */
  void SetUp() override {
    fake_upstreams_.emplace_back(
        new FakeUpstream(0, FakeHttpConnection::Type::HTTP1, version_));
    registerPort("upstream_0",
                 fake_upstreams_.back()->localAddress()->ip()->port());
    fake_upstreams_.emplace_back(
        new FakeUpstream(0, FakeHttpConnection::Type::HTTP1, version_));
    registerPort("upstream_1",
                 fake_upstreams_.back()->localAddress()->ip()->port());
    createTestServer(ConfigPath(), {"http"});
  }

  /**
   * Destructor for an individual integration test.
   */
  void TearDown() override {
    test_server_.reset();
    fake_upstreams_.clear();
  }

 protected:
  Http::TestHeaderMapImpl BaseRequestHeaders() {
    return Http::TestHeaderMapImpl{
        {":method", "GET"}, {":path", "/"}, {":authority", "host"}};
  }

  Http::TestHeaderMapImpl createHeaders(const std::string& token) {
    auto headers = BaseRequestHeaders();
    headers.addCopy("Authorization", "Bearer " + token);
    return headers;
  }

  Http::TestHeaderMapImpl createIssuerHeaders() {
    return Http::TestHeaderMapImpl{{":status", "200"}};
  }

  std::string InstanceToString(Buffer::Instance& instance) {
    auto len = instance.length();
    return std::string(static_cast<char*>(instance.linearize(len)), len);
  }

  std::map<std::string, std::string> HeadersMapToMap(
      const Http::HeaderMap& headers) {
    std::map<std::string, std::string> ret;
    headers.iterate(
        [](const Http::HeaderEntry& entry, void* context) -> void {
          auto ret = static_cast<std::map<std::string, std::string>*>(context);
          Http::LowerCaseString lower_key{entry.key().c_str()};
          (*ret)[std::string(lower_key.get())] =
              std::string(entry.value().c_str());
        },
        &ret);
    return ret;
  };

  void ExpectHeaderIncluded(const Http::HeaderMap& headers1,
                            const Http::HeaderMap& headers2) {
    auto map1 = HeadersMapToMap(headers1);
    auto map2 = HeadersMapToMap(headers2);
    for (const auto& kv : map1) {
      EXPECT_EQ(map2[kv.first], kv.second);
    }
  }

  void TestVerification(const Http::HeaderMap& request_headers,
                        const std::string& request_body,
                        const Http::HeaderMap& issuer_response_headers,
                        const std::string& issuer_response_body,
                        bool verification_success,
                        const Http::HeaderMap& expected_headers,
                        const std::string& expected_body) {
    IntegrationCodecClientPtr codec_client;
    FakeHttpConnectionPtr fake_upstream_connection_issuer;
    FakeHttpConnectionPtr fake_upstream_connection_backend;
    IntegrationStreamDecoderPtr response(
        new IntegrationStreamDecoder(*dispatcher_));
    FakeStreamPtr request_stream_issuer;
    FakeStreamPtr request_stream_backend;

    codec_client = makeHttpConnection(lookupPort("http"));

    // Send a request to Envoy.
    if (!request_body.empty()) {
      Http::StreamEncoder& encoder =
          codec_client->startRequest(request_headers, *response);
      Buffer::OwnedImpl body(request_body);
      codec_client->sendData(encoder, body, true);
    } else {
      codec_client->makeHeaderOnlyRequest(request_headers, *response);
    }

    fake_upstream_connection_issuer =
        fake_upstreams_[1]->waitForHttpConnection(*dispatcher_);
    request_stream_issuer = fake_upstream_connection_issuer->waitForNewStream();
    request_stream_issuer->waitForEndStream(*dispatcher_);

    // Mock a response from an issuer server.
    if (!issuer_response_body.empty()) {
      request_stream_issuer->encodeHeaders(issuer_response_headers, false);
      Buffer::OwnedImpl body(issuer_response_body);
      request_stream_issuer->encodeData(body, true);
    } else {
      request_stream_issuer->encodeHeaders(issuer_response_headers, true);
    }

    // Valid JWT case.
    // Check if the request sent to the backend includes the expected one.
    if (verification_success) {
      fake_upstream_connection_backend =
          fake_upstreams_[0]->waitForHttpConnection(*dispatcher_);
      request_stream_backend =
          fake_upstream_connection_backend->waitForNewStream();
      request_stream_backend->waitForEndStream(*dispatcher_);

      EXPECT_TRUE(request_stream_backend->complete());

      ExpectHeaderIncluded(expected_headers, request_stream_backend->headers());
      if (!expected_body.empty()) {
        EXPECT_EQ(expected_body,
                  InstanceToString(request_stream_backend->body()));
      }
    }

    response->waitForEndStream();

    // Invalid JWT case.
    // Check if the response sent to the client includes the expected one.
    if (!verification_success) {
      EXPECT_TRUE(response->complete());

      ExpectHeaderIncluded(expected_headers, response->headers());
      if (!expected_body.empty()) {
        EXPECT_EQ(expected_body, response->body());
      }
    }

    codec_client->close();
    fake_upstream_connection_issuer->close();
    fake_upstream_connection_issuer->waitForDisconnect();
    if (verification_success) {
      fake_upstream_connection_backend->close();
      fake_upstream_connection_backend->waitForDisconnect();
    }
  }

 private:
  virtual std::string ConfigPath() = 0;
};

class JwtVerificationFilterIntegrationTestWithJwks
    : public JwtVerificationFilterIntegrationTest {
  std::string ConfigPath() override {
    return "src/envoy/auth/integration_test/envoy.conf.jwk";
  }

 protected:
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
};

INSTANTIATE_TEST_CASE_P(
    IpVersions, JwtVerificationFilterIntegrationTestWithJwks,
    testing::ValuesIn(TestEnvironment::getIpVersionsForTest()));

TEST_P(JwtVerificationFilterIntegrationTestWithJwks, Success1) {
  const std::string kJwtNoKid =
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

  auto expected_headers = BaseRequestHeaders();
  expected_headers.addCopy("sec-istio-auth-userinfo",
                           "{\"iss\":\"https://"
                           "example.com\",\"sub\":\"test@example.com\",\"aud\":"
                           "\"example_service\",\"exp\":2001001001}");

  TestVerification(createHeaders(kJwtNoKid), "", createIssuerHeaders(),
                   kPublicKey, true, expected_headers, "");
}

TEST_P(JwtVerificationFilterIntegrationTestWithJwks, JwtExpired) {
  const std::string kJwtNoKid =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0.XYPg6VPrq-H1Kl-kgmAfGFomVpnmdZLIAo0g6dhJb2Be_"
      "koZ2T76xg5_Lr828hsLKxUfzwNxl5-k1cdz_kAst6vei0hdnOYqRQ8EhkZS_"
      "5Y2vWMrzGHw7AUPKCQvSnNqJG5HV8YdeOfpsLhQTd-"
      "tG61q39FWzJ5Ra5lkxWhcrVDQFtVy7KQrbm2dxhNEHAR2v6xXP21p1T5xFBdmGZbHFiH63N9"
      "dwdRgWjkvPVTUqxrZil7PSM2zg_GTBETp_"
      "qS7Wwf8C0V9o2KZu0KDV0j0c9nZPWTv3IMlaGZAtQgJUeyemzRDtf4g2yG3xBZrLm3AzDUj_"
      "EX_pmQAHA5ZjPVCAw";

  TestVerification(createHeaders(kJwtNoKid), "", createIssuerHeaders(),
                   kPublicKey, false,
                   Http::TestHeaderMapImpl{{":status", "401"}}, "JWT_EXPIRED");
}

TEST_P(JwtVerificationFilterIntegrationTestWithJwks, AudInvalid) {
  // Payload:
  // {"iss":"https://example.com","sub":"test@example.com","aud":"invalid_service","exp":2001001001}
  const std::string jwt =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImF1ZCI6ImludmFsaWRfc2VydmljZSIsImV4cCI6MjAwMTAwMTAwMX0."
      "gEWnuqtEdVzC94lVbuClaVLoxs-w-_uKJRbAYwAKRulE-"
      "ZhxG9VtKCd8i90xEuk9txB3tT8VGjdZKs5Hf5LjF4ebobV3M9ya6mZvq1MdcUHYiUtQhJe3M"
      "t_2sxRmogK-QZ7HcuA9hpFO4HHVypnMDr4WHgxx2op1vhKU7NDlL-"
      "38Dpf6uKEevxi0Xpids9pSST4YEQjReTXJDJECT5dhk8ZQ_lcS-pujgn7kiY99bTf6j4U-"
      "ajIcWwtQtogYx4bcmHBUvEjcYOC86TRrnArZSk1mnO7OGq4KrSrqhXnvqDmc14LfldyWqEks"
      "X5FkM94prXPK0iN-pPVhRjNZ4xvR-w";

  TestVerification(createHeaders(jwt), "", createIssuerHeaders(), kPublicKey,
                   false, Http::TestHeaderMapImpl{{":status", "401"}},
                   "ISS_AUD_UNMATCH");
}

TEST_P(JwtVerificationFilterIntegrationTestWithJwks, Fail1) {
  std::string token = "invalidToken";
  std::string pubkey = "weirdKey";
  TestVerification(createHeaders(token), "", createIssuerHeaders(), pubkey,
                   false, Http::TestHeaderMapImpl{{":status", "401"}},
                   "JWT_BAD_FORMAT");
}

/*
 * TODO: add tests
 */

}  // Envoy
