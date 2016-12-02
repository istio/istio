/* Copyright 2016 Google Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
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
