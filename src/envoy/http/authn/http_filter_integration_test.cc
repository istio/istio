#include "test/integration/http_integration.h"
#include "test/integration/utility.h"
#include "test/test_common/utility.h"

namespace Envoy {
namespace {

class AuthenticationFilterIntegrationTest
    : public HttpIntegrationTest,
      public testing::TestWithParam<Network::Address::IpVersion> {
 public:
  AuthenticationFilterIntegrationTest()
      : HttpIntegrationTest(Http::CodecClient::Type::HTTP1, GetParam()),
        default_request_headers_{
            {":method", "GET"}, {":path", "/"}, {":authority", "host"}} {}

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

// TODO (diemtvu/lei-tang): add test for MTls success.
// TODO (diemtvu/lei-tang): add test(s) for JWT authentication.

}  // namespace
}  // namespace Envoy
