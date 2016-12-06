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
