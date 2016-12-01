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
#include "src/api_manager/gce_metadata.h"

#include "google/protobuf/stubs/status.h"
#include "src/api_manager/auth/lib/json_util.h"

using ::google::api_manager::auth::GetProperty;
using ::google::api_manager::auth::GetStringValue;
using ::google::api_manager::utils::Status;
using ::google::protobuf::util::error::Code;

namespace google {
namespace api_manager {

namespace {

// Assigns a const char * to std::string safely
inline std::string SafeAssign(const char *str) { return (str) ? str : ""; }

}  // namespace

Status GceMetadata::ParseFromJson(std::string *json_str) {
  // TODO: use protobuf to parse Json.
  grpc_json *json = grpc_json_parse_string_with_len(
      const_cast<char *>(json_str->data()), json_str->length());
  if (!json) {
    return Status(Code::INVALID_ARGUMENT,
                  "Invalid JSON response from metadata server",
                  Status::INTERNAL);
  }

  const grpc_json *project = GetProperty(json, "project");
  const grpc_json *instance = GetProperty(json, "instance");
  const grpc_json *attributes = GetProperty(instance, "attributes");

  project_id_ = SafeAssign(GetStringValue(project, "projectId"));
  zone_ = SafeAssign(GetStringValue(instance, "zone"));
  gae_server_software_ =
      SafeAssign(GetStringValue(attributes, "gae_server_software"));
  kube_env_ = SafeAssign(GetStringValue(attributes, "kube-env"));

  grpc_json_destroy(json);

  // Only keep last portion of zone
  if (!zone_.empty()) {
    std::size_t last_slash = zone_.find_last_of("/");
    if (last_slash != std::string::npos) {
      zone_ = zone_.substr(last_slash + 1);
    }
  }

  return Status::OK;
}

}  // namespace api_manager
}  // namespace google
