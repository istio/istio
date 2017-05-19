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
#include "src/envoy/transcoding/filter.h"

#include "common/buffer/buffer_impl.h"
#include "common/grpc/codec.h"
#include "common/grpc/common.h"
#include "common/http/header_map_impl.h"

#include "src/envoy/transcoding/test/bookstore.pb.h"
#include "test/mocks/http/mocks.h"
#include "test/mocks/upstream/mocks.h"
#include "test/test_common/environment.h"
#include "test/test_common/printers.h"
#include "test/test_common/utility.h"

#include "gmock/gmock.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"

using testing::_;
using testing::Invoke;
using testing::NiceMock;
using testing::Return;
using testing::ReturnPointee;
using testing::ReturnRef;

using google::protobuf::util::MessageDifferencer;
using google::protobuf::util::Status;
using google::protobuf::util::error::Code;

namespace Envoy {
namespace Grpc {
namespace Transcoding {

class GrpcHttpJsonTranscodingFilterTest : public testing::Test {
 public:
  GrpcHttpJsonTranscodingFilterTest()
      : config_(*bookstoreJson()), filter_(config_) {
    filter_.setDecoderFilterCallbacks(decoder_callbacks_);
    filter_.setEncoderFilterCallbacks(encoder_callbacks_);
  }

  const Json::ObjectSharedPtr bookstoreJson() {
    std::string descriptor_path = TestEnvironment::runfilesPath(
        "src/envoy/transcoding/test/bookstore.descriptor");
    std::string json_string = "{\"proto_descriptor\": \"" + descriptor_path +
                              "\","
                              "\"services\": [\"bookstore.Bookstore\"]}";
    return Json::Factory::loadFromString(json_string);
  }

  Transcoding::Config config_;
  Transcoding::Instance filter_;
  NiceMock<Http::MockStreamDecoderFilterCallbacks> decoder_callbacks_;
  NiceMock<Http::MockStreamEncoderFilterCallbacks> encoder_callbacks_;
};

TEST_F(GrpcHttpJsonTranscodingFilterTest, NoTranscoding) {
  Http::TestHeaderMapImpl request_headers{
      {"content-type", "application/grpc"},
      {":method", "POST"},
      {":path", "/grpc.service/UnknownGrpcMethod"}};

  Http::TestHeaderMapImpl expected_request_headers{
      {"content-type", "application/grpc"},
      {":method", "POST"},
      {":path", "/grpc.service/UnknownGrpcMethod"}};

  EXPECT_EQ(Http::FilterHeadersStatus::Continue,
            filter_.decodeHeaders(request_headers, false));
  EXPECT_EQ(expected_request_headers, request_headers);
}

TEST_F(GrpcHttpJsonTranscodingFilterTest, TranscodingUnaryPost) {
  Http::TestHeaderMapImpl request_headers{{"content-type", "application/json"},
                                          {":method", "POST"},
                                          {":path", "/shelf"}};

  EXPECT_EQ(Http::FilterHeadersStatus::Continue,
            filter_.decodeHeaders(request_headers, false));
  EXPECT_EQ("application/grpc", request_headers.get_("content-type"));
  EXPECT_EQ("/bookstore.Bookstore/CreateShelf", request_headers.get_(":path"));
  EXPECT_EQ("trailers", request_headers.get_("te"));

  Buffer::OwnedImpl request_data{"{\"theme\": \"Children\"}"};

  EXPECT_EQ(Http::FilterDataStatus::Continue,
            filter_.decodeData(request_data, true));

  Decoder decoder;
  std::vector<Frame> frames;
  decoder.decode(request_data, frames);

  EXPECT_EQ(1, frames.size());

  bookstore::CreateShelfRequest expected_request;
  expected_request.mutable_shelf()->set_theme("Children");

  bookstore::CreateShelfRequest request;
  request.ParseFromArray(frames[0].data_->linearize(frames[0].length_),
                         frames[0].length_);

  EXPECT_EQ(expected_request.ByteSize(), frames[0].length_);
  EXPECT_TRUE(MessageDifferencer::Equals(expected_request, request));

  Http::TestHeaderMapImpl response_headers{{"content-type", "application/grpc"},
                                           {":status", "200"}};

  EXPECT_EQ(Http::FilterHeadersStatus::Continue,
            filter_.encodeHeaders(response_headers, false));
  EXPECT_EQ("application/json", response_headers.get_("content-type"));

  bookstore::Shelf response;
  response.set_id(20);
  response.set_theme("Children");

  auto response_data = Common::serializeBody(response);

  EXPECT_EQ(Http::FilterDataStatus::Continue,
            filter_.encodeData(*response_data, false));

  std::string response_json(
      reinterpret_cast<const char*>(
          response_data->linearize(response_data->length())),
      response_data->length());

  EXPECT_EQ("{\"id\":\"20\",\"theme\":\"Children\"}", response_json);

  Http::TestHeaderMapImpl response_trailers{{"grpc-status", "0"},
                                            {"grpc-message", ""}};

  EXPECT_EQ(Http::FilterTrailersStatus::Continue,
            filter_.decodeTrailers(response_trailers));
}

TEST_F(GrpcHttpJsonTranscodingFilterTest, TranscodingUnaryError) {
  Http::TestHeaderMapImpl request_headers{{"content-type", "application/json"},
                                          {":method", "POST"},
                                          {":path", "/shelf"}};

  EXPECT_EQ(Http::FilterHeadersStatus::Continue,
            filter_.decodeHeaders(request_headers, false));
  EXPECT_EQ("application/grpc", request_headers.get_("content-type"));
  EXPECT_EQ("/bookstore.Bookstore/CreateShelf", request_headers.get_(":path"));
  EXPECT_EQ("trailers", request_headers.get_("te"));

  Buffer::OwnedImpl request_data{"{\"theme\": \"Children\""};

  EXPECT_CALL(decoder_callbacks_, encodeHeaders_(_, false))
      .WillOnce(Invoke([](Http::HeaderMap& headers, bool end_stream) {
        EXPECT_STREQ("400", headers.Status()->value().c_str());
      }));
  EXPECT_CALL(decoder_callbacks_, encodeData(_, true));

  EXPECT_EQ(Http::FilterDataStatus::StopIterationNoBuffer,
            filter_.decodeData(request_data, true));
  EXPECT_EQ(0, request_data.length());
}

}  // namespace Transcoding
}  // namespace Grpc
}  // namespace Envoy
