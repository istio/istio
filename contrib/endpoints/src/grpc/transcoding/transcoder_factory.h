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
#ifndef GRPC_TRANSCODING_TRANSODER_FACTORY_H_
#define GRPC_TRANSCODING_TRANSODER_FACTORY_H_

#include <memory>

#include "google/api/service.pb.h"
#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/stubs/status.h"
#include "include/api_manager/method_call_info.h"
#include "src/grpc/transcoding/transcoder.h"
#include "src/grpc/transcoding/type_helper.h"

namespace google {
namespace api_manager {
namespace transcoding {

// Transcoder factory for a specific service config. Holds the preprocessed
// service config and creates a Transcoder per each client request using the
// following information:
//  - method call information
//    - RPC method info
//      - request & response message types
//      - request & response streaming flags
//    - HTTP body field path
//    - request variable bindings
//      - Values for certain fields to be injected into the request message.
//  - request input stream for the request JSON coming from the client,
//  - response input stream for the response proto coming from the backend.
//
// EXAMPLE:
//   TranscoderFactory factory(service_config);
//
//   ::google::protobuf::io::ZeroCopyInputStream *request_downstream =
//      CreateClientJsonRequestStream();
//
//   ::google::protobuf::io::ZeroCopyInputStream *response_upstream =
//      CreateBackendProtoResponseStream();
//
//   unique_ptr<Transcoder> transcoder;
//   status = factory.Create(request_info,
//                           request_downstream,
//                           response_upstream,
//                           &transcoder);
//
class TranscoderFactory {
 public:
  // service - The service config for which the factory is created
  TranscoderFactory(const ::google::api::Service& service_config);

  // Creates a Transcoder object to transcode a single client request
  // call_info - contains all the necessary info for setting up transcoding
  // request_input - ZeroCopyInputStream that carries the JSON request coming
  //                 from the client
  // response_input - ZeroCopyInputStream that carries the proto response
  //                  coming from the backend
  // transcoder - the output Transcoder object
  ::google::protobuf::util::Status Create(
      const MethodCallInfo& call_info,
      ::google::protobuf::io::ZeroCopyInputStream* request_input,
      ::google::protobuf::io::ZeroCopyInputStream* response_input,
      std::unique_ptr<Transcoder>* transcoder);

 private:
  TypeHelper type_helper_;
};

}  // namespace transcoding
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_TRANSODER_FACTORY_H_
