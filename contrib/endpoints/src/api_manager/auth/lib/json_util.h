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
#ifndef API_MANAGER_AUTH_LIB_JSON_UTIL_H_
#define API_MANAGER_AUTH_LIB_JSON_UTIL_H_

// This header file is for auth library internal use only
// since it directly includes a grpc header file.
// A public header file should not include any grpc header files.

#include "src/api_manager/auth/lib/grpc_internals.h"

namespace google {
namespace api_manager {
namespace auth {

// Gets given JSON property by key name.
const grpc_json *GetProperty(const grpc_json *json, const char *key);

// Gets string value by key or nullptr if no such key or property is not string
// type.
const char *GetStringValue(const grpc_json *json, const char *key);

// Gets a value of a number property with a given key, or nullptr if no such key
// exists or the property is property is not number type.
const char *GetNumberValue(const grpc_json *json, const char *key);

// Fill grpc_child with key, value and type, and setup links from/to
// brother/parents.
void FillChild(grpc_json *child, grpc_json *brother, grpc_json *parent,
               const char *key, const char *value, grpc_json_type type);

}  // namespace auth
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_AUTH_LIB_JSON_UTIL_H_
