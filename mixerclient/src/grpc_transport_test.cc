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

#include <grpc++/security/server_credentials.h>
#include <grpc++/server.h>
#include <grpc++/server_builder.h>
#include <grpc++/server_context.h>
#include <grpc/grpc.h>
#include "gmock/gmock.h"
#include "gtest/gtest.h"

#include <future>

using grpc::Server;
using grpc::ServerBuilder;
using grpc::ServerContext;
using grpc::ServerReaderWriter;

using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::istio::mixer::v1::QuotaRequest;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::util::Status;

using ::testing::Invoke;
using ::testing::_;

namespace istio {
namespace mixer_client {

// A fake Mixer gRPC server: just echo a response for each request.
class MockMixerServerImpl final : public ::istio::mixer::v1::Mixer::Service {
 public:
  grpc::Status Check(
      ServerContext* context,
      ServerReaderWriter<CheckResponse, CheckRequest>* stream) override {
    CheckRequest request;
    while (stream->Read(&request)) {
      CheckResponse response;
      // Just echo it back with same request_index.
      response.set_request_index(request.request_index());
      stream->Write(response);
    }
    return grpc::Status::OK;
  }
  grpc::Status Report(
      ServerContext* context,
      ServerReaderWriter<ReportResponse, ReportRequest>* stream) override {
    ReportRequest request;
    while (stream->Read(&request)) {
      ReportResponse response;
      // Just echo it back with same request_index.
      response.set_request_index(request.request_index());
      stream->Write(response);
    }
    return grpc::Status::OK;
  }
  grpc::Status Quota(
      ServerContext* context,
      ServerReaderWriter<QuotaResponse, QuotaRequest>* stream) override {
    QuotaRequest request;
    while (stream->Read(&request)) {
      QuotaResponse response;
      // Just echo it back with same request_index.
      response.set_request_index(request.request_index());
      stream->Write(response);
    }
    return grpc::Status::OK;
  }
};

template <class T>
class MockReader : public ReadInterface<T> {
 public:
  MOCK_METHOD1_T(OnRead, void(const T&));
  MOCK_METHOD1_T(OnClose, void(const Status&));
};

class GrpcTransportTest : public ::testing::Test {
 public:
  void SetUp() {
    // TODO: pick a un-used port. If this port is used, the test will fail.
    std::string server_address("0.0.0.0:50051");
    ServerBuilder builder;
    builder.AddListeningPort(server_address, grpc::InsecureServerCredentials());
    builder.RegisterService(&service_);
    server_ = builder.BuildAndStart();

    grpc_transport_.reset(new GrpcTransport(server_address));
  }

  void TearDown() { server_->Shutdown(); }

  // server side
  MockMixerServerImpl service_;
  std::unique_ptr<Server> server_;

  // client side
  std::unique_ptr<GrpcTransport> grpc_transport_;
};

TEST_F(GrpcTransportTest, TestSuccessCheck) {
  MockReader<CheckResponse> mock_reader;
  auto writer = grpc_transport_->NewStream(&mock_reader);

  CheckResponse response;
  EXPECT_CALL(mock_reader, OnRead(_))
      .WillOnce(Invoke([&response](const CheckResponse& r) { response = r; }));

  std::promise<Status> status_promise;
  std::future<Status> status_future = status_promise.get_future();
  EXPECT_CALL(mock_reader, OnClose(_))
      .WillOnce(Invoke([&status_promise](Status status) {
        std::promise<Status> moved_promise(std::move(status_promise));
        moved_promise.set_value(status);
      }));

  CheckRequest request;
  request.set_request_index(111);
  writer->Write(request);

  // Close the stream
  writer->WritesDone();

  // Wait for OnClose() to be called.
  status_future.wait();
  EXPECT_TRUE(status_future.get().ok());
  EXPECT_EQ(response.request_index(), request.request_index());
}

}  // namespace mixer_client
}  // namespace istio
