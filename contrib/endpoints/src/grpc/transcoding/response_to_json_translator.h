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
#ifndef GRPC_TRANSCODING_RESPONSE_TO_JSON_TRANSLATOR_H_
#define GRPC_TRANSCODING_RESPONSE_TO_JSON_TRANSLATOR_H_

#include <string>

#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/stubs/status.h"
#include "google/protobuf/util/type_resolver.h"
#include "src/grpc/transcoding/message_reader.h"
#include "src/grpc/transcoding/message_stream.h"

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
      std::string type_url, bool streaming,
      ::google::protobuf::io::ZeroCopyInputStream* in);

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
