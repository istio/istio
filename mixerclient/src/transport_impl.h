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

#ifndef MIXERCLIENT_TRANSPORT_IMPL_H
#define MIXERCLIENT_TRANSPORT_IMPL_H

#include "include/client.h"

namespace istio {
namespace mixer_client {

// Use stream transport to support ping-pong requests in the form of:
//    Call(request, response, on_done)
template <class RequestType, class ResponseType>
class StreamTransport : public ReadInterface<ResponseType> {
 public:
  StreamTransport(TransportInterface* transport) : transport_(transport) {}

  // Make a ping-pong call.
  void Call(const RequestType& request, ResponseType* response,
            DoneFunc on_done) {
    if (transport_ == nullptr) {
      on_done(::google::protobuf::util::Status(
          ::google::protobuf::util::error::Code::INVALID_ARGUMENT,
          "transport is NULL."));
      return;
    }
    if (!writer_) {
      writer_ = transport_->NewStream(this);
    }
    pair_map_.emplace(request.request_index(), Data{response, on_done});
    writer_->Write(request);
  }

 private:
  // Will be called by transport layer when receiving a response
  void OnRead(const ResponseType& response) {
    auto it = pair_map_.find(response.request_index());
    if (it == pair_map_.end()) {
      GOOGLE_LOG(ERROR) << "Failed in find request for index: "
                        << response.request_index();
      return;
    }

    if (it->second.response) {
      *it->second.response = response;
    }
    it->second.on_done(::google::protobuf::util::Status::OK);

    pair_map_.erase(it);
  }

  // Will be called by transport layer when the stream is closed.
  void OnClose(const ::google::protobuf::util::Status& status) {
    for (const auto& it : pair_map_) {
      if (status.ok()) {
        // The stream is nicely closed, but response is not received.
        it.second.on_done(::google::protobuf::util::Status(
            ::google::protobuf::util::error::Code::DATA_LOSS,
            "Response is missing."));
      } else {
        it.second.on_done(status);
      }
    }
    pair_map_.clear();
    writer_.reset();
  }

  // The transport interface to create a new stream.
  TransportInterface* transport_;
  // The writer object for current stream.
  std::unique_ptr<WriteInterface<RequestType>> writer_;
  // Store response data and callback.
  struct Data {
    ResponseType* response;
    DoneFunc on_done;
  };
  // The map to pair request with response.
  std::map<int64_t, Data> pair_map_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_TRANSPORT_IMPL_H
