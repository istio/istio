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

#include "gmock/gmock.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::istio::mixer::v1::QuotaRequest;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::TextFormat;
using ::google::protobuf::util::MessageDifferencer;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::Invoke;
using ::testing::Mock;
using ::testing::An;
using ::testing::_;

namespace istio {
namespace mixer_client {

class MockTransport : public TransportInterface {
 public:
  MOCK_METHOD1(NewStream, CheckWriterPtr(CheckReaderRawPtr));
  MOCK_METHOD1(NewStream, ReportWriterPtr(ReportReaderRawPtr));
  MOCK_METHOD1(NewStream, QuotaWriterPtr(QuotaReaderRawPtr));
};

template <class T>
class MockWriter : public WriteInterface<T> {
 public:
  MOCK_METHOD1_T(Write, void(const T&));
  MOCK_METHOD0_T(WriteDone, void());
};

class MixerClientImplTest : public ::testing::Test {
 public:
  void SetUp() {
    MixerClientOptions options(
        CheckOptions(1 /*entries */, 500 /* refresh_interval_ms */,
                     1000 /* expiration_ms */),
        ReportOptions(1 /* entries */, 500 /*flush_interval_ms*/),
        QuotaOptions(1 /* entries */, 500 /*flush_interval_ms*/,
                     1000 /* expiration_ms */));
    options.transport = &mock_transport_;
    client_ = CreateMixerClient(options);
  }

  MockTransport mock_transport_;
  std::unique_ptr<MixerClient> client_;
};

TEST_F(MixerClientImplTest, TestSingleCheck) {
  MockWriter<CheckRequest>* writer = new MockWriter<CheckRequest>;
  CheckReaderRawPtr reader;

  EXPECT_CALL(mock_transport_, NewStream(An<CheckReaderRawPtr>()))
      .WillOnce(
          Invoke([writer, &reader](CheckReaderRawPtr r) -> CheckWriterPtr {
            reader = r;
            return CheckWriterPtr(writer);
          }));

  CheckRequest request_out;
  EXPECT_CALL(*writer, Write(_))
      .WillOnce(
          Invoke([&request_out](const CheckRequest& r) { request_out = r; }));

  CheckRequest request_in;
  request_in.set_request_index(111);
  CheckResponse response_in;
  response_in.set_request_index(request_in.request_index());

  CheckResponse response_out;
  Status status_out = Status::UNKNOWN;
  client_->Check(request_in, &response_out,
                 [&status_out](Status status) { status_out = status; });
  // Write request.
  EXPECT_TRUE(MessageDifferencer::Equals(request_in, request_out));
  // But not response
  EXPECT_FALSE(status_out.ok());
  EXPECT_FALSE(MessageDifferencer::Equals(response_in, response_out));

  // Write response.
  reader->OnRead(response_in);

  EXPECT_TRUE(status_out.ok());
  EXPECT_TRUE(MessageDifferencer::Equals(response_in, response_out));
}

TEST_F(MixerClientImplTest, TestTwoOutOfOrderChecks) {
  MockWriter<CheckRequest>* writer = new MockWriter<CheckRequest>;
  CheckReaderRawPtr reader;

  EXPECT_CALL(mock_transport_, NewStream(An<CheckReaderRawPtr>()))
      .WillOnce(
          Invoke([writer, &reader](CheckReaderRawPtr r) -> CheckWriterPtr {
            reader = r;
            return CheckWriterPtr(writer);
          }));
  EXPECT_CALL(*writer, Write(_)).Times(2);

  CheckRequest request_in_1;
  request_in_1.set_request_index(111);
  CheckResponse response_in_1;
  response_in_1.set_request_index(request_in_1.request_index());

  CheckRequest request_in_2;
  request_in_2.set_request_index(222);
  CheckResponse response_in_2;
  response_in_2.set_request_index(request_in_2.request_index());

  CheckResponse response_out_1;
  client_->Check(request_in_1, &response_out_1, [](Status status) {});
  CheckResponse response_out_2;
  client_->Check(request_in_2, &response_out_2, [](Status status) {});

  // Write response in wrong order
  reader->OnRead(response_in_2);
  reader->OnRead(response_in_1);

  EXPECT_TRUE(MessageDifferencer::Equals(response_in_1, response_out_1));
  EXPECT_TRUE(MessageDifferencer::Equals(response_in_2, response_out_2));
}

TEST_F(MixerClientImplTest, TestCheckWithStreamClose) {
  MockWriter<CheckRequest>* writer = new MockWriter<CheckRequest>;
  CheckReaderRawPtr reader;

  EXPECT_CALL(mock_transport_, NewStream(An<CheckReaderRawPtr>()))
      .WillOnce(
          Invoke([writer, &reader](CheckReaderRawPtr r) -> CheckWriterPtr {
            reader = r;
            return CheckWriterPtr(writer);
          }));
  EXPECT_CALL(*writer, Write(_)).Times(1);

  CheckRequest request_in;
  request_in.set_request_index(111);

  CheckResponse response_out;
  Status status_out = Status::UNKNOWN;
  client_->Check(request_in, &response_out,
                 [&status_out](Status status) { status_out = status; });

  // Close the stream
  reader->OnClose(Status::CANCELLED);

  // Status should be canclled.
  EXPECT_EQ(status_out, Status::CANCELLED);
}

}  // namespace mixer_client
}  // namespace istio
