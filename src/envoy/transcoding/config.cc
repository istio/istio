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
#include "src/envoy/transcoding/config.h"

#include <fstream>

#include "contrib/endpoints/src/grpc/transcoding/json_request_translator.h"
#include "contrib/endpoints/src/grpc/transcoding/response_to_json_translator.h"
#include "envoy/common/exception.h"
#include "envoy/http/filter.h"
#include "google/protobuf/descriptor.h"
#include "google/protobuf/descriptor.pb.h"
#include "google/protobuf/util/type_resolver.h"
#include "google/protobuf/util/type_resolver_util.h"
#include "server/config/network/http_connection_manager.h"

using google::api_manager::transcoding::JsonRequestTranslator;
using google::api_manager::transcoding::RequestInfo;
using google::api_manager::transcoding::ResponseToJsonTranslator;
using google::api_manager::transcoding::Transcoder;
using google::api_manager::transcoding::TranscoderInputStream;
using google::protobuf::DescriptorPool;
using google::protobuf::FileDescriptor;
using google::protobuf::FileDescriptorSet;
using google::protobuf::io::ZeroCopyInputStream;
using google::protobuf::util::error::Code;
using google::protobuf::util::Status;

namespace Grpc {
namespace Transcoding {

namespace {

const std::string kTypeUrlPrefix{"type.googleapis.com"};

// Transcoder implementation based on JsonRequestTranslator &
// ResponseToJsonTranslator
class TranscoderImpl : public Transcoder {
 public:
  // request_translator - a JsonRequestTranslator that does the request
  //                      translation
  // response_translator - a ResponseToJsonTranslator that does the response
  //                       translation
  TranscoderImpl(std::unique_ptr<JsonRequestTranslator> request_translator,
                 std::unique_ptr<ResponseToJsonTranslator> response_translator)
      : request_translator_(std::move(request_translator)),
        response_translator_(std::move(response_translator)),
        request_stream_(request_translator_->Output().CreateInputStream()),
        response_stream_(response_translator_->CreateInputStream()) {}

  // Transcoder implementation
  TranscoderInputStream* RequestOutput() { return request_stream_.get(); }
  Status RequestStatus() { return request_translator_->Output().Status(); }

  ZeroCopyInputStream* ResponseOutput() { return response_stream_.get(); }
  Status ResponseStatus() { return response_translator_->Status(); }

 private:
  std::unique_ptr<JsonRequestTranslator> request_translator_;
  std::unique_ptr<ResponseToJsonTranslator> response_translator_;
  std::unique_ptr<TranscoderInputStream> request_stream_;
  std::unique_ptr<TranscoderInputStream> response_stream_;
};
}

Config::Config(const Json::Object& config, Server::Instance& server) {
  std::string proto_descriptor_file = config.getString("proto_descriptor");
  std::fstream input(proto_descriptor_file, std::ios::in | std::ios::binary);
  FileDescriptorSet descriptor_set;
  if (!descriptor_set.ParseFromIstream(&input)) {
    throw EnvoyException("Unable to parse proto descriptor");
  }
  log().debug("transcoding filter loaded");
  for (const auto& file : descriptor_set.file()) {
    if (descriptor_pool_.BuildFile(file) == nullptr) {
      throw EnvoyException("Unable to parse proto descriptor");
    }
  }
  resolver_.reset(google::protobuf::util::NewTypeResolverForDescriptorPool(
      kTypeUrlPrefix, &descriptor_pool_));
  info_.reset(google::protobuf::util::converter::TypeInfo::NewTypeInfo(
      resolver_.get()));
}

Status Config::CreateTranscoder(const Http::HeaderMap& headers,
                                ZeroCopyInputStream* request_input,
                                TranscoderInputStream* response_input,
                                std::unique_ptr<Transcoder>* transcoder) {
  std::string path = headers.Path()->value().c_str();

  auto method = ResolveMethod(headers.Method()->value().c_str(),
                              headers.Path()->value().c_str());
  if (!method) {
    return Status(Code::NOT_FOUND,
                  "Could not resolve " + path + " to a method");
  }

  RequestInfo request_info;
  auto status = MethodToRequestInfo(method, &request_info);
  if (!status.ok()) {
    return status;
  }

  std::unique_ptr<JsonRequestTranslator> request_translator{
      new JsonRequestTranslator(resolver_.get(), request_input, request_info,
                                method->client_streaming(), true)};

  auto response_type_url =
      kTypeUrlPrefix + "/" + method->output_type()->full_name();
  std::unique_ptr<ResponseToJsonTranslator> response_translator{
      new ResponseToJsonTranslator(resolver_.get(), response_type_url,
                                   method->server_streaming(), response_input)};

  transcoder->reset(new TranscoderImpl(std::move(request_translator),
                                       std::move(response_translator)));
  return Status::OK;
}

Status Config::MethodToRequestInfo(
    const google::protobuf::MethodDescriptor* method,
    google::api_manager::transcoding::RequestInfo* info) {
  // TODO: support variable bindings

  auto request_type_url =
      kTypeUrlPrefix + "/" + method->input_type()->full_name();
  info->message_type = info_->GetTypeByTypeUrl(request_type_url);
  if (info->message_type == nullptr) {
    log().debug("Cannot resolve input-type: {}",
                method->input_type()->full_name());
    return Status(Code::NOT_FOUND, "Could not resolve type: " +
                                       method->input_type()->full_name());
  }

  return Status::OK;
}

const google::protobuf::MethodDescriptor* Config::ResolveMethod(
    const std::string& method, const std::string& path) {
  auto sep = path.find('/', 1);
  if (sep == std::string::npos) {
    log().debug("No separator");
    return nullptr;
  }

  // TODO: support path bindings
  std::string service_name = path.substr(1, sep - 1);
  auto service = descriptor_pool_.FindServiceByName(service_name);
  if (service == nullptr) {
    return nullptr;
  }

  std::string method_name = path.substr(sep + 1);
  return service->FindMethodByName(method_name);
}

}  // namespace Transcoding
}  // namespace Grpc
