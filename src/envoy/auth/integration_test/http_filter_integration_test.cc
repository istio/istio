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
