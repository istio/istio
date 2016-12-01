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

#include "google/protobuf/io/zero_copy_stream_impl_lite.h"
#include "google/protobuf/util/json_util.h"
#include "google/protobuf/util/type_resolver.h"
#include "google/protobuf/util/type_resolver_util.h"

using ::google::protobuf::Message;
using ::google::protobuf::util::TypeResolver;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {
namespace utils {

namespace {
const char kTypeUrlPrefix[] = "type.googleapis.com";

// Creation function used by static lazy init.
TypeResolver* CreateTypeResolver() {
  return ::google::protobuf::util::NewTypeResolverForDescriptorPool(
      kTypeUrlPrefix, ::google::protobuf::DescriptorPool::generated_pool());
}

// Returns the singleton type resolver, creating it on first call.
TypeResolver* GetTypeResolver() {
  static TypeResolver* resolver = CreateTypeResolver();
  return resolver;
}
}  // namespace

std::string GetTypeUrl(const Message& message) {
  return std::string(kTypeUrlPrefix) + "/" +
         message.GetDescriptor()->full_name();
}

Status ProtoToJson(const Message& message, std::string* result, int options) {
  ::google::protobuf::util::JsonPrintOptions json_options;
  if (options & JsonOptions::PRETTY_PRINT) {
    json_options.add_whitespace = true;
  }
  if (options & JsonOptions::OUTPUT_DEFAULTS) {
    json_options.always_print_primitive_fields = true;
  }
  // TODO: Skip going to bytes and use ProtoObjectSource directly.
  ::google::protobuf::util::Status status =
      ::google::protobuf::util::BinaryToJsonString(
          GetTypeResolver(), GetTypeUrl(message), message.SerializeAsString(),
          result, json_options);
  return Status::FromProto(status);
}

Status ProtoToJson(const Message& message,
                   ::google::protobuf::io::ZeroCopyOutputStream* json,
                   int options) {
  ::google::protobuf::util::JsonPrintOptions json_options;
  if (options & JsonOptions::PRETTY_PRINT) {
    json_options.add_whitespace = true;
  }
  if (options & JsonOptions::OUTPUT_DEFAULTS) {
    json_options.always_print_primitive_fields = true;
  }
  // TODO: Skip going to bytes and use ProtoObjectSource directly.
  std::string binary = message.SerializeAsString();
  ::google::protobuf::io::ArrayInputStream binary_stream(binary.data(),
                                                         binary.size());
  ::google::protobuf::util::Status status =
      ::google::protobuf::util::BinaryToJsonStream(
          GetTypeResolver(), GetTypeUrl(message), &binary_stream, json,
          json_options);
  return Status::FromProto(status);
}

Status JsonToProto(const std::string& json, Message* message) {
  ::google::protobuf::util::JsonParseOptions options;
  options.ignore_unknown_fields = true;
  std::string binary;
  ::google::protobuf::util::Status status =
      ::google::protobuf::util::JsonToBinaryString(
          GetTypeResolver(), GetTypeUrl(*message), json, &binary, options);
  if (!status.ok()) {
    return Status::FromProto(status);
  }
  if (message->ParseFromString(binary)) {
    return Status::OK;
  }
  return Status(
      Code::INTERNAL,
      "Unable to parse bytes generated from JsonToBinaryString as proto.");
}

Status JsonToProto(::google::protobuf::io::ZeroCopyInputStream* json,
                   ::google::protobuf::Message* message) {
  ::google::protobuf::util::JsonParseOptions options;
  options.ignore_unknown_fields = true;
  std::string binary;
  ::google::protobuf::io::StringOutputStream output(&binary);
  ::google::protobuf::util::Status status =
      ::google::protobuf::util::JsonToBinaryStream(
          GetTypeResolver(), GetTypeUrl(*message), json, &output, options);

  if (!status.ok()) {
    return Status::FromProto(status);
  }
  if (message->ParseFromString(binary)) {
    return Status::OK;
  }
  return Status(
      Code::INTERNAL,
      "Unable to parse bytes generated from JsonToBinaryString as proto.");
}

}  // namespace utils
}  // namespace api_manager
}  // namespace google
