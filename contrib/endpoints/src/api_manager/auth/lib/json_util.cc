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
