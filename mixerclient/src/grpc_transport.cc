/* Copyright 2017 Istio Authors. All Rights Reserved.
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
#include "src/grpc_transport.h"

#include <condition_variable>
#include <mutex>
#include <queue>
#include <thread>

namespace istio {
namespace mixer_client {
namespace {

// A gRPC stream
template <class RequestType, class ResponseType>
class GrpcStream final : public WriteInterface<RequestType> {
 public:
  typedef std::unique_ptr<
      ::grpc::ClientReaderWriterInterface<RequestType, ResponseType>>
      StreamPtr;
  typedef std::function<StreamPtr(::grpc::ClientContext&)> StreamNewFunc;

  GrpcStream(ReadInterface<ResponseType>* reader, StreamNewFunc create_func)
      : reader_(reader), write_closed_(false) {
    context_.set_fail_fast(true);
    stream_ = create_func(context_);
  }

  static void Start(
      std::shared_ptr<GrpcStream<RequestType, ResponseType>> grpc_stream) {
    std::thread read_t([grpc_stream]() { grpc_stream->ReadMainLoop(); });
    read_t.detach();
    std::thread write_t([grpc_stream]() { grpc_stream->WriteMainLoop(); });
    write_t.detach();
  }

  void Write(const RequestType& request) override {
    // make a copy and push to the queue
    WriteQueuePush(new RequestType(request));
  }

  void WritesDone() override {
    // push a nullptr to indicate half close
    WriteQueuePush(nullptr);
  }

  bool is_write_closed() const override {
    std::lock_guard<std::mutex> lock(write_closed_mutex_);
    return write_closed_;
  }

 private:
  // The worker loop to read response messages.
  void ReadMainLoop() {
    ResponseType response;
    while (stream_->Read(&response)) {
      reader_->OnRead(response);
    }
    ::grpc::Status status = stream_->Finish();
    GOOGLE_LOG(INFO) << "Stream Finished with status: "
                     << status.error_message();

    // Notify Write thread to quit.
    set_write_closed();
    WriteQueuePush(nullptr);

    // Convert grpc status to protobuf status.
    ::google::protobuf::util::Status pb_status(
        ::google::protobuf::util::error::Code(status.error_code()),
        ::google::protobuf::StringPiece(status.error_message()));
    reader_->OnClose(pb_status);
  }

  void WriteQueuePush(RequestType* request) {
    std::unique_lock<std::mutex> lk(write_queue_mutex_);
    write_queue_.emplace(request);
    cv_.notify_one();
  }

  std::unique_ptr<RequestType> WriteQueuePop() {
    std::unique_lock<std::mutex> lk(write_queue_mutex_);
    while (write_queue_.empty()) {
      cv_.wait(lk);
    }
    auto ret = std::move(write_queue_.front());
    write_queue_.pop();
    return ret;
  }

  void set_write_closed() {
    std::lock_guard<std::mutex> lock(write_closed_mutex_);
    write_closed_ = true;
  }

  // The worker loop to write request message.
  void WriteMainLoop() {
    while (true) {
      auto request = WriteQueuePop();
      if (!request) {
        if (!is_write_closed()) {
          stream_->WritesDone();
          set_write_closed();
        }
        break;
      }
      if (!stream_->Write(*request)) {
        set_write_closed();
        break;
      }
    }
  }

  // The client context.
  ::grpc::ClientContext context_;

  // The reader writer stream.
  StreamPtr stream_;
  // The reader interface from caller.
  ReadInterface<ResponseType>* reader_;

  // Indicates if write is closed.
  mutable std::mutex write_closed_mutex_;
  bool write_closed_;

  // Mutex to protect write queue.
  std::mutex write_queue_mutex_;
  // condition to wait for write_queue.
  std::condition_variable cv_;
  // a queue to store pending queue for write
  std::queue<std::unique_ptr<RequestType>> write_queue_;
};

typedef GrpcStream<::istio::mixer::v1::CheckRequest,
                   ::istio::mixer::v1::CheckResponse>
    CheckGrpcStream;
typedef GrpcStream<::istio::mixer::v1::ReportRequest,
                   ::istio::mixer::v1::ReportResponse>
    ReportGrpcStream;
typedef GrpcStream<::istio::mixer::v1::QuotaRequest,
                   ::istio::mixer::v1::QuotaResponse>
    QuotaGrpcStream;

}  // namespace

GrpcTransport::GrpcTransport(const std::string& mixer_server) {
  channel_ = CreateChannel(mixer_server, ::grpc::InsecureChannelCredentials());
  stub_ = ::istio::mixer::v1::Mixer::NewStub(channel_);
}

CheckWriterPtr GrpcTransport::NewStream(CheckReaderRawPtr reader) {
  auto writer = std::make_shared<CheckGrpcStream>(
      reader,
      [this](::grpc::ClientContext& context) -> CheckGrpcStream::StreamPtr {
        return stub_->Check(&context);
      });
  CheckGrpcStream::Start(writer);
  return writer;
}

ReportWriterPtr GrpcTransport::NewStream(ReportReaderRawPtr reader) {
  auto writer = std::make_shared<ReportGrpcStream>(
      reader,
      [this](::grpc::ClientContext& context) -> ReportGrpcStream::StreamPtr {
        return stub_->Report(&context);
      });
  ReportGrpcStream::Start(writer);
  return writer;
}

QuotaWriterPtr GrpcTransport::NewStream(QuotaReaderRawPtr reader) {
  auto writer = std::make_shared<QuotaGrpcStream>(
      reader,
      [this](::grpc::ClientContext& context) -> QuotaGrpcStream::StreamPtr {
        return stub_->Quota(&context);
      });
  QuotaGrpcStream::Start(writer);
  return writer;
}

}  // namespace mixer_client
}  // namespace istio
