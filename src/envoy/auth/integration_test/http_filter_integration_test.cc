#include "test/integration/integration.h"
#include "test/integration/utility.h"

namespace Envoy {
class JwtVerificationFilterIntegrationTest
    : public BaseIntegrationTest,
      public testing::TestWithParam<Network::Address::IpVersion> {
 public:
  JwtVerificationFilterIntegrationTest() : BaseIntegrationTest(GetParam()) {}
  /**
   * Initializer for an individual integration test.
   */
  void SetUp() override {
    fake_upstreams_.emplace_back(
        new FakeUpstream(0, FakeHttpConnection::Type::HTTP1, version_));
    registerPort("upstream_0",
                 fake_upstreams_.back()->localAddress()->ip()->port());
    createTestServer("src/envoy/auth/integration_test/envoy.conf", {"http"});
  }

  /**
   * Destructor for an individual integration test.
   */
  void TearDown() override {
    test_server_.reset();
    fake_upstreams_.clear();
  }
};

INSTANTIATE_TEST_CASE_P(
    IpVersions, JwtVerificationFilterIntegrationTest,
    testing::ValuesIn(TestEnvironment::getIpVersionsForTest()));

/*
 * A trivial test. Just making connection and sending a request, testing
 * nothing.
 */
TEST_P(JwtVerificationFilterIntegrationTest, Trivial) {
  Http::TestHeaderMapImpl headers{
      {":method", "GET"}, {":path", "/"}, {":authority", "host"}};

  IntegrationCodecClientPtr codec_client;
  FakeHttpConnectionPtr fake_upstream_connection;
  IntegrationStreamDecoderPtr response(
      new IntegrationStreamDecoder(*dispatcher_));
  FakeStreamPtr request_stream;

  codec_client =
      makeHttpConnection(lookupPort("http"), Http::CodecClient::Type::HTTP1);
  codec_client->makeHeaderOnlyRequest(headers, *response);
  fake_upstream_connection =
      fake_upstreams_[0]->waitForHttpConnection(*dispatcher_);
  request_stream = fake_upstream_connection->waitForNewStream();
  request_stream->waitForEndStream(*dispatcher_);
  response->waitForEndStream();

  codec_client->close();
}
}  // Envoy
