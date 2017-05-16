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

#pragma once

#include "common/common/logger.h"
#include "envoy/json/json_object.h"
#include "envoy/server/instance.h"
#include "google/protobuf/descriptor.h"
#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/util/internal/type_info.h"
#include "google/protobuf/util/type_resolver.h"
#include "src/path_matcher.h"
#include "src/request_message_translator.h"
#include "src/transcoder.h"
#include "src/type_helper.h"

namespace Envoy {
namespace Grpc {
namespace Transcoding {

class Instance;

class MethodInfo {
 public:
  MethodInfo(const google::protobuf::MethodDescriptor* method)
      : method_(method) {}
  const std::set<std::string> system_query_parameter_names() const {
    return std::set<std::string>();
  }
  const google::protobuf::MethodDescriptor* method() const { return method_; }

 private:
  const google::protobuf::MethodDescriptor* method_;
};

// VariableBinding specifies a value for a single field in the request message.
// When transcoding HTTP/REST/JSON to gRPC/proto the request message is
// constructed using the HTTP body and the variable bindings (specified through
// request url).
struct VariableBinding {
  // The location of the field in the protobuf message, where the value
  // needs to be inserted, e.g. "shelf.theme" would mean the "theme" field
  // of the nested "shelf" message of the request protobuf message.
  std::vector<std::string> field_path;
  // The value to be inserted.
  std::string value;
};

class Config : public Logger::Loggable<Logger::Id::config> {
 public:
  Config(const Json::Object& config);

  google::protobuf::util::Status CreateTranscoder(
      const Http::HeaderMap& headers,
      google::protobuf::io::ZeroCopyInputStream* request_input,
      google::grpc::transcoding::TranscoderInputStream* response_input,
      std::unique_ptr<google::grpc::transcoding::Transcoder>& transcoder,
      const google::protobuf::MethodDescriptor*& method_descriptor);

  google::protobuf::util::Status MethodToRequestInfo(
      const google::protobuf::MethodDescriptor* method,
      google::grpc::transcoding::RequestInfo* info);

 private:
  google::protobuf::DescriptorPool descriptor_pool_;
  google::grpc::transcoding::PathMatcherPtr<MethodInfo*> path_matcher_;
  std::vector<std::unique_ptr<MethodInfo>> methods_;
  std::unique_ptr<google::grpc::transcoding::TypeHelper> type_helper_;
};

typedef std::shared_ptr<Config> ConfigSharedPtr;

}  // namespace Transcoding
}  // namespace Grpc
}  // namespace Envoy
