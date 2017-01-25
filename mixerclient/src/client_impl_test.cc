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

#include "include/client.h"

#include "gtest/gtest.h"

using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::istio::mixer::v1::QuotaRequest;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

// A simple writer to send response right away.
template <class RequestType, class ResponseType>
class WriterImpl : public WriteInterface<RequestType> {
 public:
  void Write(const RequestType& request) {
    ResponseType response;
    response.set_request_index(request.request_index());
    if (done_status.ok()) {
      reader->OnRead(response);
    }
    reader->OnClose(done_status);
  }
  void WritesDone() {}
  bool is_write_closed() const { return true; }

  ReadInterface<ResponseType>* reader;
  Status done_status;
};

class MixerClientImplTest : public ::testing::Test, public TransportInterface {
 public:
  MixerClientImplTest()
      : check_writer_(new WriterImpl<CheckRequest, CheckResponse>),
        report_writer_(new WriterImpl<ReportRequest, ReportResponse>),
        quota_writer_(new WriterImpl<QuotaRequest, QuotaResponse>) {
    MixerClientOptions options(
        CheckOptions(1 /*entries */, 500 /* refresh_interval_ms */,
                     1000 /* expiration_ms */),
        ReportOptions(1 /* entries */, 500 /*flush_interval_ms*/),
        QuotaOptions(1 /* entries */, 500 /*flush_interval_ms*/,
                     1000 /* expiration_ms */));
    options.transport = this;
    client_ = CreateMixerClient(options);
  }

  CheckWriterPtr NewStream(CheckReaderRawPtr reader) {
    check_writer_->reader = reader;
    return CheckWriterPtr(check_writer_.release());
  }
  ReportWriterPtr NewStream(ReportReaderRawPtr reader) {
    report_writer_->reader = reader;
    return ReportWriterPtr(report_writer_.release());
  }
  QuotaWriterPtr NewStream(QuotaReaderRawPtr reader) {
    quota_writer_->reader = reader;
    return QuotaWriterPtr(quota_writer_.release());
  }

  std::unique_ptr<MixerClient> client_;
  std::unique_ptr<WriterImpl<CheckRequest, CheckResponse>> check_writer_;
  std::unique_ptr<WriterImpl<ReportRequest, ReportResponse>> report_writer_;
  std::unique_ptr<WriterImpl<QuotaRequest, QuotaResponse>> quota_writer_;
};

TEST_F(MixerClientImplTest, TestSuccessCheck) {
  Attributes attributes;
  Status done_status = Status::UNKNOWN;
  client_->Check(attributes,
                 [&done_status](Status status) { done_status = status; });
  EXPECT_TRUE(done_status.ok());
}

TEST_F(MixerClientImplTest, TestFailedCheck) {
  check_writer_->done_status = Status::CANCELLED;
  Attributes attributes;
  Status done_status = Status::UNKNOWN;
  client_->Check(attributes,
                 [&done_status](Status status) { done_status = status; });
  EXPECT_EQ(done_status, Status::CANCELLED);
}

}  // namespace mixer_client
}  // namespace istio
