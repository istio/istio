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
#include "common/grpc/codec.h"
#include "common/grpc/common.h"
#include "src/envoy/transcoding/test/bookstore.pb.h"

#include "gtest/gtest.h"

class TranscodingIntegrationTest : public BaseIntegrationTest,
                                   public testing::Test {
 public:
  /**
   * Global initializer for all integration tests.
   */
  void SetUp() override {
    fake_upstreams_.emplace_back(
        new FakeUpstream(0, FakeHttpConnection::Type::HTTP2));
    registerPort("upstream_0",
                 fake_upstreams_.back()->localAddress()->ip()->port());
    createTestServer("src/envoy/transcoding/test/integration.json", {"http"});
  }

  /**
   * Global destructor for all integration tests.
   */
  void TearDown() override {
    test_server_.reset();
    fake_upstreams_.clear();
  }
};

TEST_F(TranscodingIntegrationTest, BasicUnary) {
  IntegrationCodecClientPtr codec_client;
  FakeHttpConnectionPtr fake_upstream_connection;
  IntegrationStreamDecoderPtr response(
      new IntegrationStreamDecoder(*dispatcher_));

  codec_client =
      makeHttpConnection(lookupPort("http"), Http::CodecClient::Type::HTTP1);
  Http::StreamEncoder& encoder = codec_client->startRequest(
      Http::TestHeaderMapImpl{{":method", "POST"},
                              {":path", "/shelf"},
                              {":authority", "host"},
                              {"content-type", "application/json"}},
      *response);
  Buffer::OwnedImpl request_data{"{\"theme\": \"Children\"}"};
  codec_client->sendData(encoder, request_data, true);

  fake_upstream_connection =
      fake_upstreams_[0]->waitForHttpConnection(*dispatcher_);
  FakeStreamPtr request_stream = fake_upstream_connection->waitForNewStream();

  request_stream->waitForEndStream(*dispatcher_);

  Grpc::Decoder grpc_decoder;
  std::vector<Grpc::Frame> frames;
  grpc_decoder.decode(request_stream->body(), frames);
  EXPECT_EQ(1, frames.size());

  bookstore::CreateShelfRequest csr;
  csr.ParseFromArray(frames[0].data_->linearize(frames[0].length_),
                     frames[0].length_);
  EXPECT_EQ("Children", csr.shelf().theme());

  request_stream->encodeHeaders(
      Http::TestHeaderMapImpl{{"content-type", "application/grpc"},
                              {":status", "200"}},
      false);

  bookstore::Shelf response_pb;
  response_pb.set_id(20);
  response_pb.set_theme("Children");

  auto response_data = Grpc::Common::serializeBody(response_pb);
  request_stream->encodeData(*response_data, false);

  request_stream->encodeTrailers(
      Http::TestHeaderMapImpl{{"grpc-status", "0"}, {"grpc-message", ""}});

  response->waitForEndStream();
  EXPECT_TRUE(request_stream->complete());
  EXPECT_TRUE(response->complete());
  EXPECT_EQ("{\"id\":\"20\",\"theme\":\"Children\"}", response->body());

  codec_client->close();
  fake_upstream_connection->close();
  fake_upstream_connection->waitForDisconnect();
}
