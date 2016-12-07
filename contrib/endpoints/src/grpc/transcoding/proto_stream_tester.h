/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef GRPC_TRANSCODING_PROTO_STREAM_TESTER_H_
#define GRPC_TRANSCODING_PROTO_STREAM_TESTER_H_

#include <string>

#include "contrib/endpoints/src/grpc/transcoding/message_stream.h"
#include "google/protobuf/stubs/status.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {

// A helper that makes it easy to test a stream of protobuf messages
// represented through a MessageStream interface. It handles matching
// proto messages, validating the GRPC message delimiter (see
// http://www.grpc.io/docs/guides/wire.html) and automatically checking the
// stream status.
class ProtoStreamTester {
 public:
  // stream - the stream to be tested
  // delimiters - whether the messages have delimiters or not
  ProtoStreamTester(MessageStream& stream, bool delimiters);

  // Validation methods
  template <typename MessageType>
  bool ExpectNextEq(const std::string& expected_proto_text);
  bool ExpectNone();
  bool ExpectFinishedEq(bool expected);
  bool ExpectStatusEq(int error_code);

 private:
  // Validates the GRPC message delimiter at the beginning
  // of the message.
  bool ValidateDelimiter(const std::string& message);

  MessageStream& stream_;
  bool delimiters_;

  static const int kDelimiterSize = 5;
};

template <typename MessageType>
bool ProtoStreamTester::ExpectNextEq(const std::string& expected_proto_text) {
  // First check the status of the stream
  if (!ExpectStatusEq(google::protobuf::util::error::OK)) {
    return false;
  }
  // Try to get a message
  std::string message;
  if (!stream_.NextMessage(&message)) {
    ADD_FAILURE() << "ProtoStreamTester::ValidateNext: NextMessage() "
                     "returned false\n";
    // Use ExpectStatusEq() to output the status if it's not OK.
    ExpectStatusEq(google::protobuf::util::error::OK);
    return false;
  }
  // Validate the delimiter if it's expected
  if (delimiters_) {
    if (!ValidateDelimiter(message)) {
      return false;
    } else {
      // Strip the delimiter
      message = message.substr(kDelimiterSize);
    }
  }
  // Parse the actual message
  MessageType actual;
  if (!actual.ParseFromString(message)) {
    ADD_FAILURE() << "ProtoStreamTester::ValidateNext: couldn't parse "
                     "the actual message:\n"
                  << message << std::endl;
    return false;
  }
  // Parse the expected message
  MessageType expected;
  if (!google::protobuf::TextFormat::ParseFromString(expected_proto_text,
                                                     &expected)) {
    ADD_FAILURE() << "ProtoStreamTester::ValidateNext: couldn't parse "
                     "the expected message:\n"
                  << expected_proto_text << std::endl;
    return false;
  }
  // Now try matching the protos
  if (!google::protobuf::util::MessageDifferencer::Equivalent(expected,
                                                              actual)) {
    // Use EXPECT_EQ on debug strings to output the diff
    EXPECT_EQ(expected.DebugString(), actual.DebugString());
    return false;
  }
  return true;
}

}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_PROTO_STREAM_TESTER_H_
