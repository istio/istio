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

#include <mutex>
#include "include/client.h"
#include "src/attribute_converter.h"

namespace istio {
namespace mixer_client {

// A simple reader to implement ReaderInterface.
// It also handles request response pairing.
template <class ResponseType>
class ReaderImpl : public ReadInterface<ResponseType> {
 public:
  ReaderImpl() : is_closed_(false) {}

  // This callback will be called when OnClose() is called.
  void SetOnCloseCallback(std::function<void()> on_close) {
    on_close_ = on_close;
  }

  void AddRequest(int64_t request_index, ResponseType* response,
                  DoneFunc on_done) {
    DoneFunc closed_on_done;
    {
      std::unique_lock<std::mutex> lock(map_mutex_);
      if (is_closed_) {
        closed_on_done = on_done;
      } else {
        pair_map_.emplace(request_index, Data{response, on_done});
      }
    }
    if (closed_on_done) {
      // The stream is already closed, calls its on_done with error.
      closed_on_done(::google::protobuf::util::Status(
          ::google::protobuf::util::error::Code::DATA_LOSS,
          "Stream is already closed."));
    }
  }

  // Will be called by transport layer when receiving a response
  void OnRead(const ResponseType& response) {
    DoneFunc on_done;
    {
      std::unique_lock<std::mutex> lock(map_mutex_);
      auto it = pair_map_.find(response.request_index());
      if (it == pair_map_.end()) {
        GOOGLE_LOG(ERROR) << "Failed in find request for index: "
                          << response.request_index();
        return;
      }

      if (it->second.response) {
        *it->second.response = response;
      }
      on_done = it->second.on_done;
      pair_map_.erase(it);
    }
    on_done(::google::protobuf::util::Status::OK);
  }

  // Will be called by transport layer when the stream is closed.
  void OnClose(const ::google::protobuf::util::Status& status) {
    std::vector<DoneFunc> on_done_array;
    {
      std::unique_lock<std::mutex> lock(map_mutex_);
      for (const auto& it : pair_map_) {
        on_done_array.push_back(it.second.on_done);
      }
      pair_map_.clear();
      is_closed_ = true;
    }

    for (auto on_done : on_done_array) {
      if (status.ok()) {
        // The stream is nicely closed, but response is not received.
        on_done(::google::protobuf::util::Status(
            ::google::protobuf::util::error::Code::DATA_LOSS,
            "Response is missing."));
      } else {
        on_done(status);
      }
    }

    // on_close_ should be properly released since it owns this
    // ReaderImpl object.
    std::function<void()> tmp_on_close;
    tmp_on_close.swap(on_close_);
    if (tmp_on_close) {
      tmp_on_close();
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
  // Mutex guarding the access of pair_map_ and is_closed_;
  std::mutex map_mutex_;
  // The map to pair request with response.
  std::map<int64_t, Data> pair_map_;
  // Indicates the stream is closed.
  bool is_closed_;
};

// Use stream transport to support ping-pong requests in the form of:
//    Call(request, response, on_done)
template <class RequestType, class ResponseType>
class StreamTransport {
 public:
  StreamTransport(TransportInterface* transport,
                  AttributeConverter<RequestType>* converter)
      : transport_(transport), converter_(converter) {}

  // Make a ping-pong call.
  void Call(const Attributes& attributes, ResponseType* response,
            DoneFunc on_done) {
    if (transport_ == nullptr) {
      on_done(::google::protobuf::util::Status(
          ::google::protobuf::util::error::Code::INVALID_ARGUMENT,
          "transport is NULL."));
      return;
    }
    std::shared_ptr<WriteInterface<RequestType>> writer = writer_.lock();
    std::shared_ptr<ReaderImpl<ResponseType>> reader = reader_.lock();
    if (!writer || !reader || writer->is_write_closed()) {
      reader = std::make_shared<ReaderImpl<ResponseType>>();
      auto writer_unique_ptr = transport_->NewStream(reader.get());
      // Transfer writer ownership to shared_ptr.
      writer.reset(writer_unique_ptr.release());
      reader_ = reader;
      writer_ = writer;
      // Reader and Writer objects are owned by the OnClose callback.
      // After the OnClose() is released, they will be released.
      reader->SetOnCloseCallback([reader, writer]() {});
    }
    RequestType request;
    // Cast the writer raw pointer as StreamID.
    converter_->FillProto(reinterpret_cast<StreamID>(writer.get()), attributes,
                          &request);
    reader->AddRequest(request.request_index(), response, on_done);
    writer->Write(request);
  }

 private:
  // The transport interface to create a new stream.
  TransportInterface* transport_;
  // Attribute converter.
  AttributeConverter<RequestType>* converter_;
  // The reader object for current stream.
  // The object is owned by Reader OnClose callback function.
  std::weak_ptr<ReaderImpl<ResponseType>> reader_;
  // The writer object for current stream.
  // The object is owned by Reader OnClose callback function.
  std::weak_ptr<WriteInterface<RequestType>> writer_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_STREAM_TRANSPORT_H
