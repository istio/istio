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
#include "contrib/endpoints/src/grpc/transcoding/request_translator_test_base.h"

#include <fstream>
#include <memory>
#include <sstream>
#include <string>
#include <vector>

#include "contrib/endpoints/src/grpc/transcoding/message_stream.h"
#include "contrib/endpoints/src/grpc/transcoding/test_common.h"
#include "google/api/service.pb.h"
#include "google/protobuf/stubs/strutil.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/internal/type_info.h"
#include "gtest/gtest.h"

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
