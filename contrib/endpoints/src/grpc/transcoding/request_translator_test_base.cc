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
#include "src/grpc/transcoding/request_translator_test_base.h"

#include <fstream>
#include <memory>
#include <sstream>
#include <string>
#include <vector>

#include "google/api/service.pb.h"
#include "google/protobuf/stubs/strutil.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/internal/type_info.h"
#include "gtest/gtest.h"
#include "src/grpc/transcoding/message_stream.h"
#include "src/grpc/transcoding/test_common.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {
namespace {

// Parse a dot delimited field path string into a vector of actual field
// pointers
std::vector<const google::protobuf::Field*> ParseFieldPath(
    const google::protobuf::Type& type,
    google::protobuf::util::converter::TypeInfo& type_info,
    const std::string& field_path_str) {
  // First, split the field names
  auto field_names = google::protobuf::Split(field_path_str, ".");

  auto current_type = &type;
  std::vector<const google::protobuf::Field*> field_path;
  for (size_t i = 0; i < field_names.size(); ++i) {
    // Find the field by name
    auto field = type_info.FindField(current_type, field_names[i]);
    EXPECT_NE(nullptr, field) << "Could not find field " << field_names[i]
                              << " in type " << current_type->name()
                              << " while parsing field path " << field_path_str
                              << std::endl;
    field_path.push_back(field);

    if (i < field_names.size() - 1) {
      // If it's not the last one in the path, it must be a message
      EXPECT_EQ(google::protobuf::Field::TYPE_MESSAGE, field->kind())
          << "Encountered a non-leaf field " << field->name()
          << " that is not a message while parsing field path" << field_path_str
          << std::endl;

      // Update the type of the current field
      current_type = type_info.GetTypeByTypeUrl(field->type_url());
      EXPECT_NE(nullptr, current_type)
          << "Could not resolve type url " << field->type_url()
          << " of the field " << field_names[i] << " while parsing field path "
          << field_path_str << std::endl;
    }
  }
  return field_path;
}

}  // namespace

RequestTranslatorTestBase::RequestTranslatorTestBase()
    : type_(),
      body_prefix_(),
      bindings_(),
      output_delimiters_(false),
      tester_() {}

RequestTranslatorTestBase::~RequestTranslatorTestBase() {}

void RequestTranslatorTestBase::LoadService(
    const std::string& config_pb_txt_file) {
  EXPECT_TRUE(transcoding::testing::LoadService(config_pb_txt_file, &service_));
  type_helper_.reset(new TypeHelper(service_.types(), service_.enums()));
}

void RequestTranslatorTestBase::SetMessageType(const std::string& type_name) {
  type_ = type_helper_->Info()->GetTypeByTypeUrl("type.googleapis.com/" +
                                                 type_name);
  EXPECT_NE(nullptr, type_) << "Could not resolve the message type "
                            << type_name << std::endl;
}

void RequestTranslatorTestBase::AddVariableBinding(
    const std::string& field_path_str, std::string value) {
  auto field_path =
      ParseFieldPath(*type_, *type_helper_->Info(), field_path_str);
  bindings_.emplace_back(
      RequestWeaver::BindingInfo{field_path, std::move(value)});
}

void RequestTranslatorTestBase::Build() {
  RequestInfo request_info;
  request_info.message_type = type_;
  request_info.body_field_path = body_prefix_;
  request_info.variable_bindings = bindings_;

  auto output_stream = Create(*type_helper_->Resolver(), output_delimiters_,
                              std::move(request_info));

  tester_.reset(new ProtoStreamTester(*output_stream, output_delimiters_));
}

}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
