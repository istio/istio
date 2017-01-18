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

#include "src/stream_transport.h"

#include "gmock/gmock.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::MessageDifferencer;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;
using ::testing::An;
using ::testing::Invoke;
using ::testing::Mock;
using ::testing::Return;
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
  MOCK_METHOD0_T(is_write_closed, bool());
};

class TransportImplTest : public ::testing::Test {
 public:
  TransportImplTest() : stream_(&mock_transport_) {}

  MockTransport mock_transport_;
  StreamTransport<CheckRequest, CheckResponse> stream_;
};

TEST_F(TransportImplTest, TestSingleCheck) {
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
  stream_.Call(request_in, &response_out,
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

  reader->OnClose(Status::OK);
}

TEST_F(TransportImplTest, TestTwoOutOfOrderChecks) {
  MockWriter<CheckRequest>* writer = new MockWriter<CheckRequest>;
  CheckReaderRawPtr reader;

  EXPECT_CALL(mock_transport_, NewStream(An<CheckReaderRawPtr>()))
      .WillOnce(
          Invoke([writer, &reader](CheckReaderRawPtr r) -> CheckWriterPtr {
            reader = r;
            return CheckWriterPtr(writer);
          }));
  EXPECT_CALL(*writer, Write(_)).Times(2);
  EXPECT_CALL(*writer, is_write_closed()).WillOnce(Return(false));

  // Send two requests: 1 and 2
  // But OnRead() is called out of order: 2 and 1
  CheckRequest request_in_1;
  request_in_1.set_request_index(111);
  CheckResponse response_in_1;
  response_in_1.set_request_index(request_in_1.request_index());

  CheckRequest request_in_2;
  request_in_2.set_request_index(222);
  CheckResponse response_in_2;
  response_in_2.set_request_index(request_in_2.request_index());

  CheckResponse response_out_1;
  stream_.Call(request_in_1, &response_out_1, [](Status status) {});
  CheckResponse response_out_2;
  stream_.Call(request_in_2, &response_out_2, [](Status status) {});

  // Write response in wrong order
  reader->OnRead(response_in_2);
  reader->OnRead(response_in_1);

  EXPECT_TRUE(MessageDifferencer::Equals(response_in_1, response_out_1));
  EXPECT_TRUE(MessageDifferencer::Equals(response_in_2, response_out_2));

  reader->OnClose(Status::OK);
}

TEST_F(TransportImplTest, TestCheckWithStreamClose) {
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
  stream_.Call(request_in, &response_out,
               [&status_out](Status status) { status_out = status; });

  // Close the stream
  reader->OnClose(Status::CANCELLED);

  // Status should be canclled.
  EXPECT_EQ(status_out, Status::CANCELLED);
}

TEST_F(TransportImplTest, TestHalfClose) {
  MockWriter<CheckRequest>* writers[2] = {new MockWriter<CheckRequest>,
                                          new MockWriter<CheckRequest>};
  CheckReaderRawPtr readers[2];
  int idx = 0;

  EXPECT_CALL(mock_transport_, NewStream(An<CheckReaderRawPtr>()))
      .WillRepeatedly(Invoke(
          [&writers, &readers, &idx](CheckReaderRawPtr r) -> CheckWriterPtr {
            readers[idx] = r;
            return CheckWriterPtr(writers[idx++]);
          }));
  EXPECT_CALL(*writers[0], Write(_)).Times(1);
  // Half close the first stream
  EXPECT_CALL(*writers[0], is_write_closed()).WillOnce(Return(true));
  EXPECT_CALL(*writers[1], Write(_)).Times(1);

  // Send the first request.
  CheckRequest request_in_1;
  request_in_1.set_request_index(111);
  CheckResponse response_in_1;
  response_in_1.set_request_index(request_in_1.request_index());

  CheckResponse response_out_1;
  stream_.Call(request_in_1, &response_out_1, [](Status status) {});

  // Send the second request
  CheckRequest request_in_2;
  request_in_2.set_request_index(222);
  CheckResponse response_in_2;
  response_in_2.set_request_index(request_in_2.request_index());

  CheckResponse response_out_2;
  stream_.Call(request_in_2, &response_out_2, [](Status status) {});

  // Write responses
  readers[1]->OnRead(response_in_2);
  readers[0]->OnRead(response_in_1);

  EXPECT_TRUE(MessageDifferencer::Equals(response_in_1, response_out_1));
  EXPECT_TRUE(MessageDifferencer::Equals(response_in_2, response_out_2));

  readers[1]->OnClose(Status::OK);
  readers[0]->OnClose(Status::OK);
}

}  // namespace mixer_client
}  // namespace istio
