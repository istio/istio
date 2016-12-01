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
#include "src/api_manager/auth/lib/json_util.h"
#include <stddef.h>
#include <string.h>

extern "C" {
#include "grpc/support/log.h"
}

namespace google {
namespace api_manager {
namespace auth {

namespace {

bool isNullOrEmpty(const char *str) { return str == nullptr || *str == '\0'; }

}  // namespace

const grpc_json *GetProperty(const grpc_json *json, const char *key) {
  if (json == nullptr || key == nullptr) {
    return nullptr;
  }
  const grpc_json *cur;
  for (cur = json->child; cur != nullptr; cur = cur->next) {
    if (strcmp(cur->key, key) == 0) return cur;
  }
  return nullptr;
}

const char *GetPropertyValue(const grpc_json *json, const char *key,
                             grpc_json_type type) {
  const grpc_json *cur = GetProperty(json, key);
  if (cur != nullptr) {
    if (cur->type != type) {
      gpr_log(GPR_ERROR, "Unexpected type of a %s field [%s]: %d", key,
              cur->value, type);
      return nullptr;
    }
    return cur->value;
  }
  return nullptr;
}

const char *GetStringValue(const grpc_json *json, const char *key) {
  return GetPropertyValue(json, key, GRPC_JSON_STRING);
}

const char *GetNumberValue(const grpc_json *json, const char *key) {
  return GetPropertyValue(json, key, GRPC_JSON_NUMBER);
}

void FillChild(grpc_json *child, grpc_json *brother, grpc_json *parent,
               const char *key, const char *value, grpc_json_type type) {
  if (isNullOrEmpty(key) || isNullOrEmpty(value)) {
    return;
  }

  memset(child, 0, sizeof(grpc_json));

  if (brother) brother->next = child;
  if (!parent->child) parent->child = child;

  child->parent = parent;
  child->key = key;
  child->value = value;
  child->type = type;
}

}  // namespace auth
}  // namespace api_manager
}  // namespace google
