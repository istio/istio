/* * Copyright (C) Extensible Service Proxy Authors
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
#ifndef GRPC_TRANSCODING_JSON_REQUEST_TRANSLATOR_H_
#define GRPC_TRANSCODING_JSON_REQUEST_TRANSLATOR_H_

#include <memory>

#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/util/internal/json_stream_parser.h"
#include "google/protobuf/util/type_resolver.h"
#include "src/grpc/transcoding/message_stream.h"
#include "src/grpc/transcoding/request_message_translator.h"
#include "src/grpc/transcoding/request_stream_translator.h"

namespace google {
namespace api_manager {

namespace transcoding {

// JsonRequestTranslator translates HTTP JSON request into gRPC message(s)
// according to the http rules defined in the service config (see
// third_party/config/google/api/http.proto).
//
// It supports streaming, weaving variable bindings and prefixing the messages
// with GRPC message delimiters (see http://www.grpc.io/docs/guides/wire.html).
// Also to support flow-control JsonRequestTranslator does the translation in a
// lazy fashion, i.e. reads the input stream only as needed.
//
// Example:
//   JsonRequestTranslator translator(type_resolver, input_json, request_info,
//                                    /*streaming*/true,
//                                    /*output_delimiters*/true);
//
//   MessageStream& out = translator.Output();
//
//   if (!out.Status().ok()) {
//     printf("Error: %s\n", out.Status().ErrorMessage().as_string().c_str());
//     return;
//   }
//
//   std::string message;
//   while (out.NextMessage(&message)) {
//     printf("Message=%s\n", message.c_str());
//   }
//
// The implementation uses JsonStreamParser to parse the incoming JSON and
// RequestMessageTranslator or RequestStreamTranslator to translate it into
// protobuf message(s).
//      - JsonStreamParser converts the incoming JSON into ObjectWriter events,
//      - in a non-streaming case RequestMessageTranslator translates these
//        events into a protobuf message,
//      - in a streaming case RequestStreamTranslator translates these events
//        into a stream of protobuf messages.
class JsonRequestTranslator {
 public:
  // type_resolver - provides type information necessary for translation (
  //                 passed to the underlying RequestMessageTranslator or
  //                 RequestStreamTranslator). Note that JsonRequestTranslator
  //                 doesn't maintain the ownership of type_resolver.
  // json_input - the input JSON stream representated through
  //              a ZeroCopyInputStream. Note that JsonRequestTranslator does
  //              not maintain the ownership of json_input.
  // request_info - information about the request being translated (passed to
  //                the underlying RequestMessageTranslator or
  //                RequestStreamTranslator).
  // streaming - whether this is a streaming call or not
  // output_delimiters - whether to ouptut gRPC message delimiters or not
  JsonRequestTranslator(::google::protobuf::util::TypeResolver* type_resolver,
                        ::google::protobuf::io::ZeroCopyInputStream* json_input,
                        RequestInfo request_info, bool streaming,
                        bool output_delimiters);

  // The translated output stream
  MessageStream& Output() { return *output_; }

 private:
  // The JSON parser
  std::unique_ptr<::google::protobuf::util::converter::JsonStreamParser>
      parser_;

  // The output stream
  std::unique_ptr<MessageStream> output_;

  // A single message translator (empty unique_ptr if this is a streaming call)
  std::unique_ptr<RequestMessageTranslator> message_translator_;

  // A message stream translator (empty unique_ptr if this is a non-streaming
  // call)
  std::unique_ptr<RequestStreamTranslator> stream_translator_;

  JsonRequestTranslator(const JsonRequestTranslator&) = delete;
  JsonRequestTranslator& operator=(const JsonRequestTranslator&) = delete;
};

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_REQUEST_TRANSLATOR_H
