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
#ifndef GRPC_TRANSCODING_JSON_REQUEST_TRANSLATOR_H_
#define GRPC_TRANSCODING_JSON_REQUEST_TRANSLATOR_H_

#include <memory>

#include "contrib/endpoints/src/grpc/transcoding/message_stream.h"
#include "contrib/endpoints/src/grpc/transcoding/request_message_translator.h"
#include "contrib/endpoints/src/grpc/transcoding/request_stream_translator.h"
#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/util/internal/json_stream_parser.h"
#include "google/protobuf/util/type_resolver.h"

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
