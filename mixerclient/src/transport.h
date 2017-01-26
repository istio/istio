/* Copyright 2017 Google Inc. All Rights Reserved.
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

#ifndef MIXERCLIENT_SRC_TRANSPORT_H
#define MIXERCLIENT_SRC_TRANSPORT_H

#include <mutex>
#include "src/attribute_context.h"
#include "src/attribute_converter.h"
#include "src/stream_transport.h"

namespace istio {
namespace mixer_client {

// A class to transport the request
template <class RequestType, class ResponseType>
class Transport : public AttributeConverter<RequestType> {
 public:
  Transport(TransportInterface* transport)
      : stream_(transport, this), last_stream_id_(0) {}

  // Send the attributes
  void Send(const Attributes& attributes, ResponseType* response,
            DoneFunc on_done) {
    stream_.Call(attributes, response, on_done);
  }

  // Convert to a protobuf
  void FillProto(StreamID stream_id, const Attributes& attributes,
                 RequestType* request) {
    std::unique_lock<std::mutex> lock(mutex_);
    if (stream_id != last_stream_id_) {
      attribute_context_.reset(new AttributeContext);
      last_stream_id_ = stream_id;
    }
    attribute_context_->FillProto(attributes,
                                  request->mutable_attribute_update());
    request->set_request_index(attribute_context_->IncRequestIndex());
  }

 private:
  // A stream transport
  StreamTransport<RequestType, ResponseType> stream_;
  // Mutex guarding the access of attribute_context and last_stream_id;
  std::mutex mutex_;
  // The attribute context for sending attributes
  std::unique_ptr<AttributeContext> attribute_context_;
  // Last used underlying stream id;
  StreamID last_stream_id_;
};

typedef Transport<::istio::mixer::v1::CheckRequest,
                  ::istio::mixer::v1::CheckResponse>
    CheckTransport;
typedef Transport<::istio::mixer::v1::ReportRequest,
                  ::istio::mixer::v1::ReportResponse>
    ReportTransport;
typedef Transport<::istio::mixer::v1::QuotaRequest,
                  ::istio::mixer::v1::QuotaResponse>
    QuotaTransport;

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_SRC_TRANSPORT_H
