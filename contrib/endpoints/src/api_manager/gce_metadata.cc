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
