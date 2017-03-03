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

const int64_t kRequestIndex1 = 111;
const int64_t kRequestIndex2 = 222;

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
  MOCK_METHOD0_T(WritesDone, void());
  MOCK_CONST_METHOD0_T(is_write_closed, bool());
};

template <class T>
class MockAttributeConverter : public AttributeConverter<T> {
 public:
  MOCK_METHOD3_T(FillProto, void(StreamID, const Attributes&, T*));
};

class TransportImplTest : public ::testing::Test {
 public:
  TransportImplTest() : stream_(&mock_transport_, &mock_converter_) {}

  MockTransport mock_transport_;
  MockAttributeConverter<CheckRequest> mock_converter_;
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

  EXPECT_CALL(mock_converter_, FillProto(_, _, _))
      .WillOnce(Invoke([](StreamID, const Attributes&, CheckRequest* request) {
        request->set_request_index(kRequestIndex1);
      }));

  CheckRequest request_out;
  EXPECT_CALL(*writer, Write(_))
      .WillOnce(
          Invoke([&request_out](const CheckRequest& r) { request_out = r; }));

  Attributes attributes;
  CheckResponse response_in;
  response_in.set_request_index(kRequestIndex1);

  CheckResponse response_out;
  Status status_out = Status::UNKNOWN;
  stream_.Call(attributes, &response_out,
               [&status_out](Status status) { status_out = status; });
  // Write request.
  EXPECT_EQ(request_out.request_index(), kRequestIndex1);
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
  int64_t index_array[2] = {kRequestIndex1, kRequestIndex2};
  int index_idx = 0;
  EXPECT_CALL(mock_converter_, FillProto(_, _, _))
      .WillRepeatedly(Invoke([&index_array, &index_idx](
          StreamID, const Attributes&, CheckRequest* request) {
        request->set_request_index(index_array[index_idx++]);
      }));

  EXPECT_CALL(*writer, Write(_)).Times(2);
  EXPECT_CALL(*writer, is_write_closed()).WillOnce(Return(false));

  Attributes attributes;
  CheckResponse response_out_1;
  stream_.Call(attributes, &response_out_1, [](Status status) {});
  CheckResponse response_out_2;
  stream_.Call(attributes, &response_out_2, [](Status status) {});

  // Write response in wrong order
  CheckResponse response_in_2;
  response_in_2.set_request_index(kRequestIndex2);
  reader->OnRead(response_in_2);

  CheckResponse response_in_1;
  response_in_1.set_request_index(kRequestIndex1);
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
  EXPECT_CALL(mock_converter_, FillProto(_, _, _))
      .WillOnce(Invoke([](StreamID, const Attributes&, CheckRequest* request) {
        request->set_request_index(kRequestIndex1);
      }));
  EXPECT_CALL(*writer, Write(_)).Times(1);

  Attributes attributes;
  CheckResponse response_out;
  Status status_out = Status::UNKNOWN;
  stream_.Call(attributes, &response_out,
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

  int64_t index_array[2] = {kRequestIndex1, kRequestIndex2};
  int index_idx = 0;
  EXPECT_CALL(mock_converter_, FillProto(_, _, _))
      .WillRepeatedly(Invoke([&index_array, &index_idx](
          StreamID, const Attributes&, CheckRequest* request) {
        request->set_request_index(index_array[index_idx++]);
      }));

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
  Attributes attributes;

  CheckResponse response_out_1;
  stream_.Call(attributes, &response_out_1, [](Status status) {});

  CheckResponse response_out_2;
  stream_.Call(attributes, &response_out_2, [](Status status) {});

  // Write responses
  CheckResponse response_in_2;
  response_in_2.set_request_index(kRequestIndex2);
  readers[1]->OnRead(response_in_2);

  CheckResponse response_in_1;
  response_in_1.set_request_index(kRequestIndex1);
  readers[0]->OnRead(response_in_1);

  EXPECT_TRUE(MessageDifferencer::Equals(response_in_1, response_out_1));
  EXPECT_TRUE(MessageDifferencer::Equals(response_in_2, response_out_2));

  readers[1]->OnClose(Status::OK);
  readers[0]->OnClose(Status::OK);
}

}  // namespace mixer_client
}  // namespace istio
