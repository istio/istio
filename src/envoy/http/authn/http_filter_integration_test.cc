#include "common/common/base64.h"
#include "src/istio/authn/context.pb.h"
#include "test/integration/http_integration.h"

using google::protobuf::util::MessageDifferencer;
using istio::authn::Payload;
using istio::authn::Result;

namespace Envoy {
namespace {
const std::string kSecIstioAuthUserInfoHeaderKey = "sec-istio-auth-userinfo";
const std::string kSecIstioAuthUserinfoHeaderValue =
    "eyJpc3MiOiI2Mjg2NDU3NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OH"
    "Z2MGxAZGV2ZWxvcGVyLmdzZXJ2aWNlYWNjb3VudC5jb20iLCJzdWIiOiI2Mjg2NDU3"
    "NDE4ODEtbm9hYml1MjNmNWE4bThvdmQ4dWN2Njk4bGo3OHZ2MGxAZGV2ZWxvcGVyLm"
    "dzZXJ2aWNlYWNjb3VudC5jb20iLCJhdWQiOiJib29rc3RvcmUtZXNwLWVjaG8uY2xv"
    "dWRlbmRwb2ludHNhcGlzLmNvbSIsImlhdCI6MTUxMjc1NDIwNSwiZXhwIjo1MTEyNz"
    "U0MjA1fQ==";
const Envoy::Http::LowerCaseString kSecIstioAuthnPayloadHeaderKey(
    "sec-istio-authn-payload");

class AuthenticationFilterIntegrationTest
    : public HttpIntegrationTest,
      public testing::TestWithParam<Network::Address::IpVersion> {
 public:
  AuthenticationFilterIntegrationTest()
      : HttpIntegrationTest(Http::CodecClient::Type::HTTP1, GetParam()),
        default_request_headers_{
            {":method", "GET"}, {":path", "/"}, {":authority", "host"}},
        request_headers_with_jwt_{{":method", "GET"},
                                  {":path", "/"},
                                  {":authority", "host"},
                                  {kSecIstioAuthUserInfoHeaderKey,
                                   kSecIstioAuthUserinfoHeaderValue}} {}

  void SetUp() override {
    fake_upstreams_.emplace_back(
        new FakeUpstream(0, FakeHttpConnection::Type::HTTP1, version_));
    registerPort("upstream_0",
                 fake_upstreams_.back()->localAddress()->ip()->port());
    fake_upstreams_.emplace_back(
        new FakeUpstream(0, FakeHttpConnection::Type::HTTP1, version_));
    registerPort("upstream_1",
                 fake_upstreams_.back()->localAddress()->ip()->port());
  }

  void TearDown() override {
    test_server_.reset();
    fake_upstreams_.clear();
  }

 protected:
  Http::TestHeaderMapImpl default_request_headers_;
  Http::TestHeaderMapImpl request_headers_with_jwt_;
};

INSTANTIATE_TEST_CASE_P(
    IpVersions, AuthenticationFilterIntegrationTest,
    testing::ValuesIn(TestEnvironment::getIpVersionsForTest()));

TEST_P(AuthenticationFilterIntegrationTest, EmptyPolicy) {
  createTestServer("src/envoy/http/authn/testdata/envoy_empty.conf", {"http"});
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  codec_client_->makeHeaderOnlyRequest(default_request_headers_, *response_);
  // Wait for request to upstream[0] (backend)
  waitForNextUpstreamRequest(0);
  // Send backend response.
  upstream_request_->encodeHeaders(Http::TestHeaderMapImpl{{":status", "200"}},
                                   true);

  response_->waitForEndStream();
  EXPECT_TRUE(response_->complete());
  EXPECT_STREQ("200", response_->headers().Status()->value().c_str());
}

TEST_P(AuthenticationFilterIntegrationTest, SourceMTlsFail) {
  createTestServer("src/envoy/http/authn/testdata/envoy_peer_mtls.conf",
                   {"http"});

  // AuthN filter use MTls, but request doesn't have certificate, request
  // would be rejected.
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  codec_client_->makeHeaderOnlyRequest(default_request_headers_, *response_);

  // Request is rejected, there will be no upstream request (thus no
  // waitForNextUpstreamRequest).
  response_->waitForEndStream();
  EXPECT_TRUE(response_->complete());
  EXPECT_STREQ("401", response_->headers().Status()->value().c_str());
}
//
//// TODO (diemtvu/lei-tang): add test for MTls success.

TEST_P(AuthenticationFilterIntegrationTest, OriginJwtRequiredHeaderNoJwtFail) {
  createTestServer(
      "src/envoy/http/authn/testdata/envoy_origin_jwt_authn_only.conf",
      {"http"});

  // The AuthN filter requires JWT, but request doesn't have JWT, request
  // would be rejected.
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  codec_client_->makeHeaderOnlyRequest(default_request_headers_, *response_);

  // Request is rejected, there will be no upstream request (thus no
  // waitForNextUpstreamRequest).
  response_->waitForEndStream();
  EXPECT_TRUE(response_->complete());
  EXPECT_STREQ("401", response_->headers().Status()->value().c_str());
}

TEST_P(AuthenticationFilterIntegrationTest, CheckValidJwtPassAuthentication) {
  createTestServer(
      "src/envoy/http/authn/testdata/envoy_origin_jwt_authn_only.conf",
      {"http"});

  // The AuthN filter requires JWT. The http request contains validated JWT and
  // the authentication should succeed.
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  codec_client_->makeHeaderOnlyRequest(request_headers_with_jwt_, *response_);

  // Wait for request to upstream[0] (backend)
  waitForNextUpstreamRequest(0);
  // Send backend response.
  upstream_request_->encodeHeaders(Http::TestHeaderMapImpl{{":status", "200"}},
                                   true);

  response_->waitForEndStream();
  EXPECT_TRUE(response_->complete());
  EXPECT_STREQ("200", response_->headers().Status()->value().c_str());
}

TEST_P(AuthenticationFilterIntegrationTest, CheckAuthnResultIsExpected) {
  createTestServer(
      "src/envoy/http/authn/testdata/envoy_origin_jwt_authn_only.conf",
      {"http"});

  // The AuthN filter requires JWT and the http request contains validated JWT.
  // In this case, the authentication should succeed and an authn result
  // should be generated.
  codec_client_ =
      makeHttpConnection(makeClientConnection((lookupPort("http"))));
  codec_client_->makeHeaderOnlyRequest(request_headers_with_jwt_, *response_);

  // Wait for request to upstream[0] (backend)
  waitForNextUpstreamRequest(0);

  // Authn result should be as expected
  const Envoy::Http::HeaderString &header_value =
      upstream_request_->headers().get(kSecIstioAuthnPayloadHeaderKey)->value();
  std::string value_base64(header_value.c_str(), header_value.size());
  const std::string value = Base64::decode(value_base64);
  Result result;
  google::protobuf::util::JsonParseOptions options;
  Result expected_result;

  bool parse_ret = result.ParseFromString(value);
  EXPECT_TRUE(parse_ret);
  JsonStringToMessage(
      R"(
          {
            "origin": {
              "user": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com/628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
              "audiences": [
               "bookstore-esp-echo.cloudendpointsapis.com"
              ],
              "presenter": "",
              "claims": {
               "aud": "bookstore-esp-echo.cloudendpointsapis.com",
               "iss": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com",
               "sub": "628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com"
              }
            }
          }
      )",
      &expected_result, options);
  // Note: TestUtility::protoEqual() uses SerializeAsString() and the output
  // is non-deterministic. Thus, MessageDifferencer::Equals() is used.
  EXPECT_TRUE(MessageDifferencer::Equals(expected_result, result));
}

}  // namespace
}  // namespace Envoy
