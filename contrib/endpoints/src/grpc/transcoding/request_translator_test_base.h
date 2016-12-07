/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef GRPC_TRANSCODING_REQUEST_TRANSLATOR_TEST_BASE_H_
#define GRPC_TRANSCODING_REQUEST_TRANSLATOR_TEST_BASE_H_

#include <memory>
#include <string>
#include <vector>

#include "contrib/endpoints/src/grpc/transcoding/message_stream.h"
#include "contrib/endpoints/src/grpc/transcoding/proto_stream_tester.h"
#include "contrib/endpoints/src/grpc/transcoding/request_message_translator.h"
#include "contrib/endpoints/src/grpc/transcoding/type_helper.h"
#include "google/api/service.pb.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/type_resolver.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {

// A base that provides common functionality for streaming and non-streaming
// translator tests.
class RequestTranslatorTestBase : public ::testing::Test {
 protected:
  RequestTranslatorTestBase();
  ~RequestTranslatorTestBase();

  // Loads the test service from a config file. Must be the first call of each
  // test.
  void LoadService(const std::string& config_pb_txt_file);

  // Methods that tests can use to build the translator
  void SetMessageType(const std::string& type_name);
  void SetBodyPrefix(const std::string& body_prefix) {
    body_prefix_ = body_prefix;
  }
  void AddVariableBinding(const std::string& field_path_str, std::string value);
  void SetOutputDelimiters(bool output_delimiters) {
    output_delimiters_ = output_delimiters;
  }
  void Build();

  // ProtoStreamTester that the tests can use to validate the output
  ProtoStreamTester& Tester() { return *tester_; }

 private:
  // Virtual Create() function that each test class must override to create the
  // translator and return the output MessageStream.
  virtual MessageStream* Create(
      google::protobuf::util::TypeResolver& type_resolver,
      bool output_delimiters, RequestInfo info) = 0;

  // The test service config
  google::api::Service service_;

  // TypeHelper for the service types (helps with resolving/navigating service
  // type information)
  std::unique_ptr<TypeHelper> type_helper_;

  // Input for building the translator
  const google::protobuf::Type* type_;
  std::string body_prefix_;
  std::vector<RequestWeaver::BindingInfo> bindings_;
  bool output_delimiters_;

  std::unique_ptr<ProtoStreamTester> tester_;
};

}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_REQUEST_TRANSLATOR_TEST_BASE_H_
