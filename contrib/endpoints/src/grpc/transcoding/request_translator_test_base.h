/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */
#ifndef GRPC_TRANSCODING_REQUEST_TRANSLATOR_TEST_BASE_H_
#define GRPC_TRANSCODING_REQUEST_TRANSLATOR_TEST_BASE_H_

#include <memory>
#include <string>
#include <vector>

#include "google/api/service.pb.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/type_resolver.h"
#include "gtest/gtest.h"
#include "src/grpc/transcoding/message_stream.h"
#include "src/grpc/transcoding/proto_stream_tester.h"
#include "src/grpc/transcoding/request_message_translator.h"
#include "src/grpc/transcoding/type_helper.h"

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
