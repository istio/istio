// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include <memory>
#include <sstream>
#include <string>
#include <vector>

#include "contrib/endpoints/include/api_manager/method.h"
#include "contrib/endpoints/include/api_manager/method_call_info.h"
#include "contrib/endpoints/src/grpc/transcoding/bookstore.pb.h"
#include "contrib/endpoints/src/grpc/transcoding/message_reader.h"
#include "contrib/endpoints/src/grpc/transcoding/test_common.h"
#include "contrib/endpoints/src/grpc/transcoding/transcoder.h"
#include "contrib/endpoints/src/grpc/transcoding/transcoder_factory.h"
#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/stubs/strutil.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {
namespace transcoding {
namespace testing {
namespace {

namespace pb = google::protobuf;
namespace pbio = google::protobuf::io;
namespace pbutil = google::protobuf::util;
namespace pberr = google::protobuf::util::error;

// MethodInfo implementation for testing. Only implements the methods that
// the TranscoderFactory needs.
class TestMethodInfo : public MethodInfo {
 public:
  TestMethodInfo() {}
  TestMethodInfo(const std::string &request_type_url,
                 const std::string &response_type_url, bool request_streaming,
                 bool response_streaming, const std::string &body_field_path)
      : request_type_url_(request_type_url),
        response_type_url_(response_type_url),
        request_streaming_(request_streaming),
        response_streaming_(response_streaming),
        body_field_path_(body_field_path) {}

  // MethodInfo implementation
  // Methods that the Transcoder doesn't use
  const std::string &name() const { return empty_; }
  const std::string &api_name() const { return empty_; }
  const std::string &api_version() const { return empty_; }
  const std::string &selector() const { return empty_; }
  bool auth() const { return false; }
  bool allow_unregistered_calls() const { return false; }
  bool isIssuerAllowed(const std::string &issuer) const { return false; }
  bool isAudienceAllowed(const std::string &issuer,
                         const std::set<std::string> &jwt_audiences) const {
    return false;
  }
  const std::vector<std::string> *http_header_parameters(
      const std::string &name) const {
    return nullptr;
  }
  const std::vector<std::string> *url_query_parameters(
      const std::string &name) const {
    return nullptr;
  }
  const std::vector<std::string> *api_key_http_headers() const {
    return nullptr;
  }
  const std::vector<std::string> *api_key_url_query_parameters() const {
    return nullptr;
  }
  const std::string &backend_address() const { return empty_; }
  const std::string &rpc_method_full_name() const { return empty_; }
  const std::set<std::string> &system_query_parameter_names() const {
    static std::set<std::string> dummy;
    return dummy;
  };

  // Methods that the Transcoder does use
  const std::string &request_type_url() const { return request_type_url_; }
  bool request_streaming() const { return request_streaming_; }
  const std::string &response_type_url() const { return response_type_url_; }
  bool response_streaming() const { return response_streaming_; }
  const std::string &body_field_path() const { return body_field_path_; }

 private:
  std::string request_type_url_;
  std::string response_type_url_;
  bool request_streaming_;
  bool response_streaming_;
  std::string body_field_path_;
  std::string empty_;
};

class TranscoderTest : public ::testing::Test {
 public:
  // Load the service config to be used for testing. This must be the first call
  // in a test.
  bool LoadService(const std::string &config_pb_txt_file) {
    if (!::google::api_manager::transcoding::testing::LoadService(
            config_pb_txt_file, &service_)) {
      return false;
    }
    transcoder_factory_.reset(new TranscoderFactory(service_));
    return true;
  }

  // Provide the method info
  void SetMethodInfo(const std::string &request_type_url,
                     const std::string &response_type_url,
                     bool request_streaming = false,
                     bool response_streaming = false,
                     const std::string &body_field_path = "") {
    method_info_.reset(new TestMethodInfo(request_type_url, response_type_url,
                                          request_streaming, response_streaming,
                                          body_field_path));
  }

  void AddVariableBinding(const std::string &field_path,
                          const std::string &value) {
    VariableBinding binding;
    binding.field_path = pb::Split(field_path, ".", /*skip_empty*/ true);
    binding.value = value;
    variable_bindings_.emplace_back(binding);
  }

  pbutil::Status Build(pbio::ZeroCopyInputStream *request_input,
                       TranscoderInputStream *response_input,
                       std::unique_ptr<Transcoder> *transcoder) {
    MethodCallInfo call_info;
    call_info.method_info = method_info_.get();
    call_info.variable_bindings = std::move(variable_bindings_);
    call_info.body_field_path = method_info_->body_field_path();

    return transcoder_factory_->Create(call_info, request_input, response_input,
                                       transcoder);
  }

 private:
  ::google::api::Service service_;
  std::unique_ptr<TranscoderFactory> transcoder_factory_;

  std::unique_ptr<TestMethodInfo> method_info_;
  std::vector<VariableBinding> variable_bindings_;
};

// A helper function that determines whether the ZeroCopyInputStream has
// finished or not.
bool IsFinished(pbio::ZeroCopyInputStream *stream) {
  const void *data = nullptr;
  int size = 0;
  if (stream->Next(&data, &size)) {
    stream->BackUp(size);
    return false;
  } else {
    return true;
  }
}

// A helper function to read all available data from a ZeroCopyInputStream
std::string ReadAll(pbio::ZeroCopyInputStream *stream) {
  std::ostringstream all;
  const void *data = nullptr;
  int size = 0;
  while (stream->Next(&data, &size) && 0 != size) {
    all << std::string(reinterpret_cast<const char *>(data),
                       static_cast<size_t>(size));
  }
  return all.str();
}

TEST_F(TranscoderTest, SimpleRequestAndResponse) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));
  SetMethodInfo(/*request_type_url*/ "type.googleapis.com/Shelf",
                /*response_type_url*/ "type.googleapis.com/Shelf");

  // Build the Transcoder
  std::unique_ptr<Transcoder> t;
  TestZeroCopyInputStream request_in, response_in;
  auto status = Build(&request_in, &response_in, &t);
  ASSERT_TRUE(status.ok()) << "Error building Transcoder - "
                           << status.error_message() << std::endl;

  // Create a MessageReader for reading & testing the request output
  MessageReader reader(t->RequestOutput());
  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_FALSE(reader.Finished());

  // Add a JSON chunk of a partial message to the request input
  request_in.AddChunk(R"({"name" : "1")");

  // Nothing yet
  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_FALSE(reader.Finished());

  // Add the rest of the message
  request_in.AddChunk(R"(, "theme" : "Fiction"})");
  request_in.Finish();

  Shelf expected;
  ASSERT_TRUE(pb::TextFormat::ParseFromString(R"(name : "1" theme : "Fiction")",
                                              &expected));

  // Read the message
  EXPECT_FALSE(reader.Finished());
  auto actual_proto = reader.NextMessage();
  ASSERT_NE(nullptr, actual_proto.get());

  // Parse & match
  Shelf actual;
  ASSERT_TRUE(actual.ParseFromZeroCopyStream(actual_proto.get()));
  EXPECT_TRUE(pbutil::MessageDifferencer::Equivalent(expected, actual));

  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_TRUE(reader.Finished());
  EXPECT_TRUE(t->RequestStatus().ok()) << "Error while translating - "
                                       << t->RequestStatus().error_message()
                                       << std::endl;

  // Now test the response translation
  EXPECT_TRUE(ReadAll(t->ResponseOutput()).empty());
  EXPECT_FALSE(IsFinished(t->ResponseOutput()));

  // Add a partial message
  auto message = GenerateGrpcMessage<Shelf>(R"(name : "2" theme : "Mystery")");
  response_in.AddChunk(message.substr(0, 10));

  // Nothing yet
  EXPECT_TRUE(ReadAll(t->ResponseOutput()).empty());
  EXPECT_FALSE(IsFinished(t->ResponseOutput()));

  // Add the rest of the message
  response_in.AddChunk(message.substr(10));
  response_in.Finish();

  // Read & test the JSON message
  auto json = ReadAll(t->ResponseOutput());
  EXPECT_TRUE(
      ExpectJsonObjectEq(R"({"name" : "2", "theme" : "Mystery"})", json));
  EXPECT_TRUE(IsFinished(t->ResponseOutput()));
}

TEST_F(TranscoderTest, RequestBindingsAndPrefix) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));
  SetMethodInfo(/*request_type_url*/ "type.googleapis.com/CreateBookRequest",
                /*response_type_url*/ "type.googleapis.com/Book",
                /*request_streaming*/ false,
                /*response_streaming*/ false,
                /*body_field_path*/ "book");
  AddVariableBinding("shelf", "99");
  AddVariableBinding("book.author", "Leo Tolstoy");
  AddVariableBinding("book.authorInfo.firstName", "Leo");
  AddVariableBinding("book.authorInfo.lastName", "Tolstoy");

  auto json = R"({
          "name" : "1",
          "title" : "War and Peace"
      }
    )";

  auto expected_proto_text =
      R"(
          shelf : 99
          book {
            name : "1"
            title : "War and Peace"
            author : "Leo Tolstoy"
            author_info {
              first_name : "Leo"
              last_name : "Tolstoy"
            }
          }
        )";

  CreateBookRequest expected;
  ASSERT_TRUE(pb::TextFormat::ParseFromString(expected_proto_text, &expected));

  // Build the Transcoder
  std::unique_ptr<Transcoder> t;
  TestZeroCopyInputStream request_in, response_in;
  auto status = Build(&request_in, &response_in, &t);
  ASSERT_TRUE(status.ok()) << "Error building Transcoder - "
                           << status.error_message() << std::endl;

  // Add input the json
  request_in.AddChunk(json);

  // Read the message
  MessageReader reader(t->RequestOutput());
  auto actual_proto = reader.NextMessage();
  ASSERT_NE(nullptr, actual_proto.get());

  // Parse & match
  CreateBookRequest actual;
  ASSERT_TRUE(actual.ParseFromZeroCopyStream(actual_proto.get()));
  EXPECT_TRUE(pbutil::MessageDifferencer::Equivalent(expected, actual));

  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_TRUE(reader.Finished());
  EXPECT_TRUE(t->RequestStatus().ok()) << "Error while translating - "
                                       << t->RequestStatus().error_message()
                                       << std::endl;
}

TEST_F(TranscoderTest, StreamingRequestAndResponse) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));
  SetMethodInfo(/*request_type_url*/ "type.googleapis.com/Shelf",
                /*response_type_url*/ "type.googleapis.com/Shelf",
                /*request_streaming*/ true,
                /*response_streaming*/ true);

  // Build the Transcoder
  std::unique_ptr<Transcoder> t;
  TestZeroCopyInputStream request_in, response_in;
  auto status = Build(&request_in, &response_in, &t);
  ASSERT_TRUE(status.ok()) << "Error building Transcoder - "
                           << status.error_message() << std::endl;

  // Add 2 complete messages and a partial one
  request_in.AddChunk(
      R"(
        [
          {"name" : "1", "theme" : "Fiction"},
          {"name" : "2", "theme" : "Satire"},
          {"name" : "3",
      )");

  // Read & test the 2 translated messages
  MessageReader reader(t->RequestOutput());

  auto actual_proto1 = reader.NextMessage();
  ASSERT_NE(nullptr, actual_proto1.get());
  Shelf actual1;
  ASSERT_TRUE(actual1.ParseFromZeroCopyStream(actual_proto1.get()));

  auto actual_proto2 = reader.NextMessage();
  ASSERT_NE(nullptr, actual_proto2.get());
  Shelf actual2;
  ASSERT_TRUE(actual2.ParseFromZeroCopyStream(actual_proto2.get()));

  Shelf expected1;
  ASSERT_TRUE(pb::TextFormat::ParseFromString(
      R"(name : "1" theme : "Fiction")", &expected1));
  EXPECT_TRUE(pbutil::MessageDifferencer::Equivalent(expected1, actual1));

  Shelf expected2;
  ASSERT_TRUE(pb::TextFormat::ParseFromString(
      R"(name : "2" theme : "Satire")", &expected2));
  EXPECT_TRUE(pbutil::MessageDifferencer::Equivalent(expected2, actual2));

  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_FALSE(reader.Finished());
  EXPECT_TRUE(t->RequestStatus().ok()) << "Error while translating - "
                                       << t->RequestStatus().error_message()
                                       << std::endl;

  // Add the rest of the 3rd message, the 4th message and close the array
  request_in.AddChunk(
      R"(
                         "theme" : "Classic"},
          {"name" : "4", "theme" : "Russian"}
        ]
      )");

  auto actual_proto3 = reader.NextMessage();
  ASSERT_NE(nullptr, actual_proto3.get());
  Shelf actual3;
  ASSERT_TRUE(actual3.ParseFromZeroCopyStream(actual_proto3.get()));

  auto actual_proto4 = reader.NextMessage();
  ASSERT_NE(nullptr, actual_proto4.get());
  Shelf actual4;
  ASSERT_TRUE(actual4.ParseFromZeroCopyStream(actual_proto4.get()));

  Shelf expected3;
  ASSERT_TRUE(pb::TextFormat::ParseFromString(
      R"(name : "3" theme : "Classic")", &expected3));
  EXPECT_TRUE(pbutil::MessageDifferencer::Equivalent(expected3, actual3));

  Shelf expected4;
  ASSERT_TRUE(pb::TextFormat::ParseFromString(
      R"(name : "4" theme : "Russian")", &expected4));
  EXPECT_TRUE(pbutil::MessageDifferencer::Equivalent(expected4, actual4));

  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_TRUE(reader.Finished());
  EXPECT_TRUE(t->RequestStatus().ok()) << "Error while translating - "
                                       << t->RequestStatus().error_message()
                                       << std::endl;

  // Test the response translation

  // Add two full messages and one partial
  auto message1 = GenerateGrpcMessage<Shelf>(R"(name : "1" theme : "Fiction")");
  auto message2 = GenerateGrpcMessage<Shelf>(R"(name : "2" theme : "Satire")");
  auto message3 = GenerateGrpcMessage<Shelf>(R"(name : "3" theme : "Classic")");
  response_in.AddChunk(message1);
  response_in.AddChunk(message2);
  response_in.AddChunk(message3.substr(0, 10));

  // Read & test the translated JSON

  auto expected12 = R"(
        [
            {"name" : "1", "theme" : "Fiction"},
            {"name" : "2", "theme" : "Satire"},
    )";

  auto actual12 = ReadAll(t->ResponseOutput());

  JsonArrayTester array_tester;
  EXPECT_TRUE(array_tester.TestChunk(expected12, actual12, false));
  EXPECT_FALSE(IsFinished(t->ResponseOutput()));
  EXPECT_TRUE(t->ResponseStatus().ok()) << "Error while translating - "
                                        << t->ResponseStatus().error_message()
                                        << std::endl;

  // Add the rest of the third message, the fourth message and finish
  response_in.AddChunk(message3.substr(10));
  auto message4 = GenerateGrpcMessage<Shelf>(R"(name : "4" theme : "Russian")");
  response_in.AddChunk(message4);
  response_in.Finish();

  // Read & test

  auto expected34 = R"(
            {"name" : "3", "theme" : "Classic"},
            {"name" : "4", "theme" : "Russian"}
        ])";

  auto actual34 = ReadAll(t->ResponseOutput());

  EXPECT_TRUE(array_tester.TestChunk(expected34, actual34, true));
  EXPECT_TRUE(IsFinished(t->ResponseOutput()));
  EXPECT_TRUE(t->ResponseStatus().ok()) << "Error while translating - "
                                        << t->ResponseStatus().error_message()
                                        << std::endl;
}

TEST_F(TranscoderTest, ErrorResolvingVariableBinding) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));
  SetMethodInfo(/*request_type_url*/ "type.googleapis.com/Shelf",
                /*response_type_url*/ "type.googleapis.com/Shelf");
  AddVariableBinding("invalid.binding", "value");

  std::unique_ptr<Transcoder> t;
  TestZeroCopyInputStream request_in, response_in;
  EXPECT_EQ(pberr::INVALID_ARGUMENT,
            Build(&request_in, &response_in, &t).error_code());
}

TEST_F(TranscoderTest, TranslationError) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));
  // Using an invalid type for simulating response translation error as it is
  // hard to generate an invalid protobuf messsage. Here we mainly test whether
  // the error is propagated correctly or not.
  SetMethodInfo(/*request_type_url*/ "type.googleapis.com/Shelf",
                /*response_type_url*/ "type.googleapis.com/InvalidType");

  // Build the Transcoder
  std::unique_ptr<Transcoder> t;
  TestZeroCopyInputStream request_in, response_in;
  auto status = Build(&request_in, &response_in, &t);
  ASSERT_TRUE(status.ok()) << "Error building Transcoder - "
                           << status.error_message() << std::endl;

  // Request error
  request_in.AddChunk(R"(Invalid JSON)");

  // Read the stream to trigger the error
  const void *buffer = nullptr;
  int size = 0;
  EXPECT_FALSE(t->RequestOutput()->Next(&buffer, &size));
  EXPECT_EQ(pberr::INVALID_ARGUMENT, t->RequestStatus().error_code());

  // Response error
  response_in.AddChunk(
      GenerateGrpcMessage<Shelf>(R"(name : "1" theme : "Fiction")"));
  EXPECT_FALSE(t->ResponseOutput()->Next(&buffer, &size));
  EXPECT_EQ(pberr::NOT_FOUND, t->ResponseStatus().error_code());
}

TEST_F(TranscoderTest, InvalidUTF8InVariableBinding) {
  ASSERT_TRUE(LoadService("bookstore_service.pb.txt"));
  SetMethodInfo(/*request_type_url*/ "type.googleapis.com/Shelf",
                /*response_type_url*/ "type.googleapis.com/Shelf");
  AddVariableBinding("theme", "\xC2\xE2\x98");

  std::unique_ptr<Transcoder> t;
  TestZeroCopyInputStream request_in, response_in;
  EXPECT_EQ(pberr::INVALID_ARGUMENT,
            Build(&request_in, &response_in, &t).error_code());
}

}  // namespace
}  // namespace testing
}  // namespace transcoding
}  // namespace api_manager
}  // namespace google
