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
#ifndef GRPC_TRANSCODING_TYPE_HELPER_H_
#define GRPC_TRANSCODING_TYPE_HELPER_H_

#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/internal/type_info.h"
#include "google/protobuf/util/type_resolver.h"

#include <memory>
#include <vector>

namespace google {
namespace api_manager {

namespace transcoding {

class SimpleTypeResolver;

// Provides ::google::protobuf::util::TypeResolver and
// ::google::protobuf::util::converter::TypeInfo implementations based on a
// collection of types and a collection of enums.
class TypeHelper {
 public:
  template <typename Types, typename Enums>
  TypeHelper(const Types& types, const Enums& enums);
  ~TypeHelper();

  ::google::protobuf::util::TypeResolver* Resolver() const;
  ::google::protobuf::util::converter::TypeInfo* Info() const;

  // Takes a string representation of a field path & resolves it into actual
  // protobuf Field pointers.
  //
  // A field path is a sequence of fields that identifies a potentially nested
  // field in the message. It can be empty as well, which identifies the entire
  // message.
  // E.g. "shelf.theme" field path would correspond to the "theme" field of the
  // "shelf" field of the top-level message. The type of the top-level message
  // is passed to ResolveFieldPath().
  //
  // The string representation of the field path is just the dot-delimited
  // list of the field names or empty:
  //    FieldPath = "" | Field {"." Field};
  //    Field     = <protobuf field name>;
  ::google::protobuf::util::Status ResolveFieldPath(
      const ::google::protobuf::Type& type, const std::string& field_path_str,
      std::vector<const ::google::protobuf::Field*>* field_path) const;

  // Resolve a field path specified through a vector of field names into a
  // vector of actual protobuf Field pointers.
  // Similiar to the above method but accepts the field path as a vector of
  // names instead of one dot-delimited string.
  ::google::protobuf::util::Status ResolveFieldPath(
      const ::google::protobuf::Type& type,
      const std::vector<std::string>& field_path_unresolved,
      std::vector<const ::google::protobuf::Field*>* field_path_resolved) const;

 private:
  void Initialize();
  void AddType(const ::google::protobuf::Type& t);
  void AddEnum(const ::google::protobuf::Enum& e);

  // We can't use a unique_ptr<SimpleTypeResolver> as the default deleter of
  // unique_ptr requires the type to be defined when the unique_ptr destructor
  // is called. In our case it's called from the template constructor below
  // (most likely as a part of stack unwinding when an exception occurs).
  SimpleTypeResolver* type_resolver_;
  std::unique_ptr<::google::protobuf::util::converter::TypeInfo> type_info_;

  TypeHelper() = delete;
  TypeHelper(const TypeHelper&) = delete;
  TypeHelper& operator=(const TypeHelper&) = delete;
};

template <typename Types, typename Enums>
TypeHelper::TypeHelper(const Types& types, const Enums& enums)
    : type_resolver_(nullptr), type_info_() {
  Initialize();
  for (const auto& t : types) {
    AddType(t);
  }
  for (const auto& e : enums) {
    AddEnum(e);
  }
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_TYPE_HELPER_H_
