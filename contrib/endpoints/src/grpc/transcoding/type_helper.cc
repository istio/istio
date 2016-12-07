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
#include "contrib/endpoints/src/grpc/transcoding/type_helper.h"

#include "google/protobuf/stubs/strutil.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/internal/type_info.h"
#include "google/protobuf/util/type_resolver.h"

#include <string>
#include <unordered_map>

namespace pb = ::google::protobuf;
namespace pbutil = ::google::protobuf::util;
namespace pbconv = ::google::protobuf::util::converter;
namespace pberr = ::google::protobuf::util::error;

namespace google {
namespace api_manager {

namespace transcoding {

const char DEFAULT_URL_PREFIX[] = "type.googleapis.com/";

class SimpleTypeResolver : public pbutil::TypeResolver {
 public:
  SimpleTypeResolver() : url_prefix_(DEFAULT_URL_PREFIX) {}

  void AddType(const pb::Type& t) {
    type_map_.emplace(url_prefix_ + t.name(), &t);
    // A temporary workaround for service configs that use
    // "proto2.MessageOptions.*" options.
    ReplaceProto2WithGoogleProtobufInOptionNames(const_cast<pb::Type*>(&t));
  }

  void AddEnum(const pb::Enum& e) {
    enum_map_.emplace(url_prefix_ + e.name(), &e);
  }

  // TypeResolver implementation
  // Resolves a type url for a message type.
  virtual pbutil::Status ResolveMessageType(const std::string& type_url,
                                            pb::Type* type) {
    auto i = type_map_.find(type_url);
    if (end(type_map_) != i) {
      if (nullptr != type) {
        *type = *i->second;
      }
      return pbutil::Status();
    } else {
      return pbutil::Status(pberr::NOT_FOUND,
                            "Type '" + type_url + "' cannot be found.");
    }
  }

  // Resolves a type url for an enum type.
  virtual pbutil::Status ResolveEnumType(const std::string& type_url,
                                         pb::Enum* enum_type) override {
    auto i = enum_map_.find(type_url);
    if (end(enum_map_) != i) {
      if (nullptr != enum_type) {
        *enum_type = *i->second;
      }
      return pbutil::Status();
    } else {
      return pbutil::Status(pberr::NOT_FOUND,
                            "Enum '" + type_url + "' cannot be found.");
    }
  }

 private:
  void ReplaceProto2WithGoogleProtobufInOptionNames(pb::Type* type) {
    // As a temporary workaround for service configs that use
    // "proto2.MessageOptions.*" options instead of
    // "google.protobuf.MessageOptions.*", we replace the option names to make
    // protobuf library recognize them.
    for (auto& option : *type->mutable_options()) {
      if (option.name() == "proto2.MessageOptions.map_entry") {
        option.set_name("google.protobuf.MessageOptions.map_entry");
      } else if (option.name() ==
                 "proto2.MessageOptions.message_set_wire_format") {
        option.set_name(
            "google.protobuf.MessageOptions.message_set_wire_format");
      }
    }
  }

  std::string url_prefix_;
  std::unordered_map<std::string, const pb::Type*> type_map_;
  std::unordered_map<std::string, const pb::Enum*> enum_map_;

  SimpleTypeResolver(const SimpleTypeResolver&) = delete;
  SimpleTypeResolver& operator=(const SimpleTypeResolver&) = delete;
};

TypeHelper::~TypeHelper() {
  type_info_.reset();
  delete type_resolver_;
}

pbutil::TypeResolver* TypeHelper::Resolver() const { return type_resolver_; }

pbconv::TypeInfo* TypeHelper::Info() const { return type_info_.get(); }

void TypeHelper::Initialize() {
  type_resolver_ = new SimpleTypeResolver();
  type_info_.reset(pbconv::TypeInfo::NewTypeInfo(type_resolver_));
}

void TypeHelper::AddType(const pb::Type& t) { type_resolver_->AddType(t); }

void TypeHelper::AddEnum(const pb::Enum& e) { type_resolver_->AddEnum(e); }

pbutil::Status TypeHelper::ResolveFieldPath(
    const pb::Type& type, const std::string& field_path_str,
    std::vector<const pb::Field*>* field_path_out) const {
  // Split the field names & call ResolveFieldPath()
  auto field_names = pb::Split(field_path_str, ".");
  return ResolveFieldPath(type, field_names, field_path_out);
}

pbutil::Status TypeHelper::ResolveFieldPath(
    const pb::Type& type, const std::vector<std::string>& field_names,
    std::vector<const pb::Field*>* field_path_out) const {
  // The type of the current message being processed (initially the type of the
  // top level message)
  auto current_type = &type;

  // The resulting field path
  std::vector<const pb::Field*> field_path;

  for (size_t i = 0; i < field_names.size(); ++i) {
    // Find the field by name in the current type
    auto field = Info()->FindField(current_type, field_names[i]);
    if (nullptr == field) {
      return pbutil::Status(pberr::INVALID_ARGUMENT,
                            "Could not find field \"" + field_names[i] +
                                "\" in the type \"" + current_type->name() +
                                "\".");
    }
    field_path.push_back(field);

    if (i < field_names.size() - 1) {
      // If this is not the last field in the path, it must be a message
      if (pb::Field::TYPE_MESSAGE != field->kind()) {
        return pbutil::Status(
            pberr::INVALID_ARGUMENT,
            "Encountered a non-leaf field \"" + field->name() +
                "\" that is not a message while parsing a field path");
      }

      // Update the type of the current message
      current_type = Info()->GetTypeByTypeUrl(field->type_url());
      if (nullptr == current_type) {
        return pbutil::Status(pberr::INVALID_ARGUMENT,
                              "Cannot find the type \"" + field->type_url() +
                                  "\" while parsing a field path.");
      }
    }
  }
  *field_path_out = std::move(field_path);
  return pbutil::Status::OK;
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
