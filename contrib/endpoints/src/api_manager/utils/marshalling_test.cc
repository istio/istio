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
#include "src/api_manager/utils/marshalling.h"

#include "google/protobuf/any.pb.h"
#include "google/protobuf/io/zero_copy_stream_impl_lite.h"
#include "google/protobuf/struct.pb.h"
#include "google/rpc/error_details.pb.h"
#include "gtest/gtest.h"
#include "include/api_manager/utils/status.h"

using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace utils {

TEST(Marshalling, ProtoToJsonDefaultOptions) {
  ::google::rpc::DebugInfo debug;
  debug.set_detail("Something went wrong.");
  debug.add_stack_entries("first");
  debug.add_stack_entries("second");

  std::string result;
  EXPECT_EQ(Status::OK, ProtoToJson(debug, &result, JsonOptions::DEFAULT));
  EXPECT_EQ(
      "{\"stackEntries\":[\"first\",\"second\"],\"detail\":\"Something went "
      "wrong.\"}",
      result);
}

TEST(Marshalling, ProtoToJsonPrettyPrintOption) {
  ::google::rpc::DebugInfo debug;
  debug.set_detail("Something went wrong.");
  debug.add_stack_entries("first");
  debug.add_stack_entries("second");

  std::string result;
  EXPECT_EQ(Status::OK, ProtoToJson(debug, &result, JsonOptions::PRETTY_PRINT));
  EXPECT_EQ(
      "{\n \"stackEntries\": [\n  \"first\",\n  \"second\"\n ],\n \"detail\": "
      "\"Something went wrong.\"\n}\n",
      result);
}

TEST(Marshalling, ProtoToJsonStream) {
  ::google::rpc::DebugInfo debug;
  debug.set_detail("Something went wrong.");
  debug.add_stack_entries("first");
  debug.add_stack_entries("second");

  std::string result;
  ::google::protobuf::io::StringOutputStream json(&result);
  EXPECT_EQ(Status::OK,
            ProtoToJson(debug, &json, JsonOptions::PRETTY_PRINT |
                                          JsonOptions::OUTPUT_DEFAULTS));
  EXPECT_EQ(
      "{\n \"stackEntries\": [\n  \"first\",\n  \"second\"\n ],\n \"detail\": "
      "\"Something went wrong.\"\n}\n",
      result);
}

TEST(Marshalling, JsonToProtoParsing) {
  std::string json =
      "{\"stackEntries\":[\"first\",\"second\"],\"detail\":\"Something went "
      "wrong.\"}";

  ::google::rpc::DebugInfo debug;
  EXPECT_EQ(Status::OK, JsonToProto(json, &debug));
  EXPECT_EQ("Something went wrong.", debug.detail());
  EXPECT_EQ(2, debug.stack_entries_size());
  EXPECT_EQ("first", debug.stack_entries(0));
  EXPECT_EQ("second", debug.stack_entries(1));
}

TEST(Marshalling, JsonToProtoParsingWithExtraFields) {
  std::string json =
      "{\"stackEntries\":[\"first\",\"second\"],\"detail\":\"Something went "
      "wrong.\", \"unknownEntries\": 0}";

  ::google::rpc::DebugInfo debug;
  EXPECT_EQ(Status::OK, JsonToProto(json, &debug));
  EXPECT_EQ("Something went wrong.", debug.detail());
  EXPECT_EQ(2, debug.stack_entries_size());
  EXPECT_EQ("first", debug.stack_entries(0));
  EXPECT_EQ("second", debug.stack_entries(1));
}

TEST(Marshalling, JsonToProtoParsingJwtPayload) {
  std::string json =
      R"({
        "iss": "23028304136-191r4v40tn4g96jf1saccr3vf1ke8aer@developer.gserviceaccount.com",
        "sub": "23028304136-191r4v40tn4g96jf1saccr3vf1ke8aer@developer.gserviceaccount.com",
        "aud": "bookstore-esp-echo.cloudendpointsapis.com",
        "iat": 1462581603,
        "exp": 1462585203
      })";

  ::google::protobuf::Struct st;

  EXPECT_EQ(Status::OK, JsonToProto(json, &st));
  EXPECT_EQ(5, st.fields_size());
  EXPECT_EQ(1462581603, static_cast<int>(st.fields().at("iat").number_value()));
  EXPECT_EQ("bookstore-esp-echo.cloudendpointsapis.com",
            st.fields().at("aud").string_value());
}

TEST(Marshalling, JsonToProtoParsingJwtPayloadWithMultipleAudiences) {
  std::string json =
      R"({
        "iss": "23028304136-191r4v40tn4g96jf1saccr3vf1ke8aer@developer.gserviceaccount.com",
        "sub": "23028304136-191r4v40tn4g96jf1saccr3vf1ke8aer@developer.gserviceaccount.com",
        "aud": ["bookstore-esp-echo.cloudendpointsapis.com", "bookstore2"],
        "iat": 1462581603,
        "exp": 1462585203
      })";

  ::google::protobuf::Struct st;

  EXPECT_EQ(Status::OK, JsonToProto(json, &st));
  EXPECT_EQ(5, st.fields_size());
  EXPECT_EQ(1462581603, static_cast<int>(st.fields().at("iat").number_value()));
  EXPECT_TRUE(st.fields().at("aud").has_list_value());
  EXPECT_EQ(2, st.fields().at("aud").list_value().values_size());
  EXPECT_EQ("bookstore-esp-echo.cloudendpointsapis.com",
            st.fields().at("aud").list_value().values(0).string_value());
}

}  // namespace utils
}  // namespace api_manager
}  // namespace google
