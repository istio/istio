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
#ifndef API_MANAGER_UTILS_MARSHALLING_H_
#define API_MANAGER_UTILS_MARSHALLING_H_

#include <string>

#include "google/protobuf/message.h"
#include "include/api_manager/utils/status.h"

namespace google {
namespace api_manager {
namespace utils {

// Options for JSON output. These should be OR'd together so must be 2^n.
enum JsonOptions {
  // Use the default behavior (useful when no options are needed).
  DEFAULT = 0,

  // Enables pretty printing of the output.
  PRETTY_PRINT = 1,

  // Prints default values for primitive fields.
  OUTPUT_DEFAULTS = 2,
};

// Returns the type URL for a protobuf Message. This is useful when embedding
// a message inside an Any, for example.
std::string GetTypeUrl(const ::google::protobuf::Message& message);

// Converts a protobuf into a JSON string. The options field is a OR'd set of
// the available JsonOptions.
// TODO: Support generating to an output buffer.
Status ProtoToJson(const ::google::protobuf::Message& message,
                   std::string* result, int options);

// Converts a protobuf into a JSON string and writes it into the output stream.
// The options parameter is an OR'd set of the available JsonOptions.
Status ProtoToJson(const ::google::protobuf::Message& message,
                   ::google::protobuf::io::ZeroCopyOutputStream* json,
                   int options);

// Converts a json string into a protobuf message.
// TODO: Support parsing directly from an input buffer.
Status JsonToProto(const std::string& json,
                   ::google::protobuf::Message* message);

// Converts a json input stream into a protobuf message.
Status JsonToProto(::google::protobuf::io::ZeroCopyInputStream* json,
                   ::google::protobuf::Message* message);

}  // namespace utils
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_UTILS_MARSHALLING_H_
