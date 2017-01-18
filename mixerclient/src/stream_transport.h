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

#ifndef MIXERCLIENT_STREAM_TRANSPORT_H
#define MIXERCLIENT_STREAM_TRANSPORT_H

#include "include/client.h"

namespace istio {
namespace mixer_client {

// A simple reader to implement ReaderInterface.
// It also handles request response pairing.
template <class ResponseType>
class ReaderImpl : public ReadInterface<ResponseType> {
 public:
  // This callback will be called when OnClose() is called.
  void SetOnCloseCallback(std::function<void()> on_close) {
    on_close_ = on_close;
  }

  void AddRequest(int64_t request_index, ResponseType* response,
                  DoneFunc on_done) {
    pair_map_.emplace(request_index, Data{response, on_done});
  }

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

    if (on_close_) {
      on_close_();
    }
  }

 private:
  // Store response data and callback.
  struct Data {
    ResponseType* response;
    DoneFunc on_done;
  };
  // The callback when the stream is closed.
  std::function<void()> on_close_;
  // The map to pair request with response.
  std::map<int64_t, Data> pair_map_;
};

// Use stream transport to support ping-pong requests in the form of:
//    Call(request, response, on_done)
template <class RequestType, class ResponseType>
class StreamTransport {
 public:
  StreamTransport(TransportInterface* transport)
      : transport_(transport), reader_(nullptr), writer_(nullptr) {}

  // Make a ping-pong call.
  void Call(const RequestType& request, ResponseType* response,
            DoneFunc on_done) {
    if (transport_ == nullptr) {
      on_done(::google::protobuf::util::Status(
          ::google::protobuf::util::error::Code::INVALID_ARGUMENT,
          "transport is NULL."));
      return;
    }
    if (!writer_ || writer_->is_write_closed()) {
      auto reader = new ReaderImpl<ResponseType>;
      auto writer_smart_ptr = transport_->NewStream(reader);
      // Both reader and writer will be freed at OnClose callback.
      auto writer = writer_smart_ptr.release();
      reader_ = reader;
      writer_ = writer;
      reader->SetOnCloseCallback([this, reader, writer]() {
        if (writer_ == writer) {
          writer_ = nullptr;
        }
        if (reader_ == reader) {
          reader_ = nullptr;
        }
        delete reader;
        delete writer;
      });
    }
    reader_->AddRequest(request.request_index(), response, on_done);
    writer_->Write(request);
  }

 private:
  // The transport interface to create a new stream.
  TransportInterface* transport_;
  // The reader object for current stream.
  ReaderImpl<ResponseType>* reader_;
  // The writer object for current stream.
  WriteInterface<RequestType>* writer_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_STREAM_TRANSPORT_H
