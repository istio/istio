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
#ifndef API_MANAGER_METHOD_CALL_INFO_H_
#define API_MANAGER_METHOD_CALL_INFO_H_

#include <string>
#include <vector>

#include "include/api_manager/method.h"

namespace google {
namespace api_manager {

// VariableBinding specifies a value for a single field in the request message.
// When transcoding HTTP/REST/JSON to gRPC/proto the request message is
// constructed using the HTTP body and the variable bindings (specified through
// request url).
struct VariableBinding {
  // The location of the field in the protobuf message, where the value
  // needs to be inserted, e.g. "shelf.theme" would mean the "theme" field
  // of the nested "shelf" message of the request protobuf message.
  std::vector<std::string> field_path;
  // The value to be inserted.
  std::string value;
};

// Information that we store per each call of a method. It includes information
// about the method itself and variable bindings for the particular call.
struct MethodCallInfo {
  // Method information
  const MethodInfo* method_info;
  // Variable bindings
  std::vector<VariableBinding> variable_bindings;
  // Body prefix (the field of the message where the HTTP body should go)
  std::string body_field_path;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_METHOD_CALL_INFO_H_
