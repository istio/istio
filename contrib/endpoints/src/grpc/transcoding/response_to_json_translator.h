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
#ifndef GRPC_TRANSCODING_RESPONSE_TO_JSON_TRANSLATOR_H_
#define GRPC_TRANSCODING_RESPONSE_TO_JSON_TRANSLATOR_H_

#include <string>

#include "contrib/endpoints/src/grpc/transcoding/message_reader.h"
#include "contrib/endpoints/src/grpc/transcoding/message_stream.h"
#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/stubs/status.h"
#include "google/protobuf/util/type_resolver.h"

namespace google {
namespace api_manager {

namespace transcoding {

// ResponseToJsonTranslator translates gRPC response message(s) into JSON. It
// accepts the input from a ZeroCopyInputStream and exposes the output through a
// MessageStream implementation. Supports streaming calls.
//
// The implementation uses a MessageReader to extract complete messages from the
// input stream and ::google::protobuf::util::BinaryToJsonStream() to do the
// actual translation. For streaming calls emits '[', ',' and ']' in appropriate
// locations to construct a JSON array.
//
// Example:
//   ResponseToJsonTranslator translator(type_resolver,
//                                       "type.googleapis.com/Shelf",
//                                       true, input_stream);
//
//   std::string message;
//   while (translator.NextMessage(&message)) {
//     printf("Message=%s\n", message.c_str());
//   }
//
//   if (!translator.Status().ok()) {
//     printf("Error: %s\n",
//            translator.Status().error_message().as_string().c_str());
//     return;
//   }
//
// NOTE: ResponseToJsonTranslator is unable to recognize the case when there is
//       an incomplete message at the end of the input. The callers will need to
//       detect it and act appropriately.
//
class ResponseToJsonTranslator : public MessageStream {
 public:
  // type_resolver - passed to BinaryToJsonStream() to do the translation
  // type_url - the type of input proto message(s)
  // streaming - whether this is a streaming call or not
  // in - the input stream of delimited proto message(s) as in the gRPC wire
  //      format (http://www.grpc.io/docs/guides/wire.html)
  ResponseToJsonTranslator(
      ::google::protobuf::util::TypeResolver* type_resolver,
      std::string type_url, bool streaming, TranscoderInputStream* in);

  // MessageStream implementation
  bool NextMessage(std::string* message);
  bool Finished() const { return finished_ || !status_.ok(); }
  ::google::protobuf::util::Status Status() const { return status_; }

 private:
  // Translates a single message
  bool TranslateMessage(::google::protobuf::io::ZeroCopyInputStream* proto_in,
                        std::string* json_out);

  ::google::protobuf::util::TypeResolver* type_resolver_;
  std::string type_url_;
  bool streaming_;

  // A MessageReader to extract full messages
  MessageReader reader_;

  // Whether this is the first message of a streaming call or not. Used to emit
  // the opening '['.
  bool first_;

  bool finished_;
  ::google::protobuf::util::Status status_;
};

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_RESPONSE_TO_JSON_TRANSLATOR_H_
