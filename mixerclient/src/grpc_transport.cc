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
#include "src/grpc_transport.h"
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
    stream_ = create_func(context_);
    worker_thread_ = std::thread([this]() { WorkerThread(); });
  }

  ~GrpcStream() { worker_thread_.join(); }

  void Write(const RequestType& request) override {
    if (!stream_->Write(request)) {
      WritesDone();
    }
  }

  void WritesDone() override {
    stream_->WritesDone();
    write_closed_ = true;
  }

  bool is_write_closed() const override { return write_closed_; }

 private:
  // The worker loop to read response messages.
  void WorkerThread() {
    ResponseType response;
    while (stream_->Read(&response)) {
      reader_->OnRead(response);
    }
    ::grpc::Status status = stream_->Finish();
    // Convert grpc status to protobuf status.
    ::google::protobuf::util::Status pb_status(
        ::google::protobuf::util::error::Code(status.error_code()),
        ::google::protobuf::StringPiece(status.error_message()));
    reader_->OnClose(pb_status);
  }

  // The client context.
  ::grpc::ClientContext context_;
  // The reader writer stream.
  StreamPtr stream_;
  // The thread to read response.
  std::thread worker_thread_;
  // The reader interface from caller.
  ReadInterface<ResponseType>* reader_;
  // Indicates if write is closed.
  bool write_closed_;
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
  return CheckWriterPtr(new CheckGrpcStream(
      reader,
      [this](::grpc::ClientContext& context) -> CheckGrpcStream::StreamPtr {
        return stub_->Check(&context);
      }));
}

ReportWriterPtr GrpcTransport::NewStream(ReportReaderRawPtr reader) {
  return ReportWriterPtr(new ReportGrpcStream(
      reader,
      [this](::grpc::ClientContext& context) -> ReportGrpcStream::StreamPtr {
        return stub_->Report(&context);
      }));
}

QuotaWriterPtr GrpcTransport::NewStream(QuotaReaderRawPtr reader) {
  return QuotaWriterPtr(new QuotaGrpcStream(
      reader,
      [this](::grpc::ClientContext& context) -> QuotaGrpcStream::StreamPtr {
        return stub_->Quota(&context);
      }));
}

}  // namespace mixer_client
}  // namespace istio
