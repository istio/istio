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
#ifndef MIXERCLIENT_GRPC_TRANSPORT_H
#define MIXERCLIENT_GRPC_TRANSPORT_H

#include <grpc++/grpc++.h>

#include "include/transport.h"
#include "mixer/api/v1/service.grpc.pb.h"

namespace istio {
namespace mixer_client {

// A gRPC implementation of Mixer transport
class GrpcTransport : public TransportInterface {
 public:
  GrpcTransport(const std::string& mixer_server);

  CheckWriterPtr NewStream(CheckReaderRawPtr reader);
  ReportWriterPtr NewStream(ReportReaderRawPtr reader);
  QuotaWriterPtr NewStream(QuotaReaderRawPtr reader);

 private:
  std::shared_ptr<::grpc::Channel> channel_;
  std::unique_ptr<::istio::mixer::v1::Mixer::Stub> stub_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_GRPC_TRANSPORT_H
