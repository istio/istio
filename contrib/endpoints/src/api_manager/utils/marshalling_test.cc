// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
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
