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
#include "contrib/endpoints/src/api_manager/path_matcher.h"
#include "contrib/endpoints/src/grpc/transcoding/request_message_translator.h"
#include "contrib/endpoints/src/grpc/transcoding/transcoder.h"
#include "envoy/json/json_object.h"
#include "envoy/server/instance.h"
#include "google/protobuf/descriptor.h"
#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/util/internal/type_info.h"
#include "google/protobuf/util/type_resolver.h"

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

class Config : public Logger::Loggable<Logger::Id::config> {
 public:
  Config(const Json::Object& config, Server::Instance& server);

  google::protobuf::util::Status CreateTranscoder(
      const Http::HeaderMap& headers,
      google::protobuf::io::ZeroCopyInputStream* request_input,
      google::api_manager::transcoding::TranscoderInputStream* response_input,
      std::unique_ptr<google::api_manager::transcoding::Transcoder>& transcoder,
      const google::protobuf::MethodDescriptor*& method_descriptor);

  google::protobuf::util::Status MethodToRequestInfo(
      const google::protobuf::MethodDescriptor* method,
      google::api_manager::transcoding::RequestInfo* info);

 private:
  google::protobuf::DescriptorPool descriptor_pool_;
  std::unique_ptr<google::protobuf::util::TypeResolver> resolver_;
  std::unique_ptr<google::protobuf::util::converter::TypeInfo> info_;
  google::api_manager::PathMatcherPtr<MethodInfo*> path_matcher_;
  std::vector<std::unique_ptr<MethodInfo>> methods_;

  friend class Instance;
};

typedef std::shared_ptr<Config> ConfigSharedPtr;

}  // namespace Transcoding
}  // namespace Grpc
