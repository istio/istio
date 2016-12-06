// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/grpc/transcoding/proto_stream_tester.h"

#include <string>

#include "google/protobuf/stubs/status.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {

ProtoStreamTester::ProtoStreamTester(MessageStream& stream, bool delimiters)
    : stream_(stream), delimiters_(delimiters) {}

bool ProtoStreamTester::ExpectNone() {
  // First check the status of the stream
  if (!ExpectStatusEq(google::protobuf::util::error::OK)) {
    return false;
  }
  std::string message;
  if (stream_.NextMessage(&message)) {
    ADD_FAILURE() << "ProtoStreamTester::ValidateNone: NextMessage() returned "
                     "true; expected false.\n";
    return false;
  }
  return true;
}

bool ProtoStreamTester::ExpectFinishedEq(bool expected) {
  // First check the status of the stream
  if (!ExpectStatusEq(google::protobuf::util::error::OK)) {
    return false;
  }
  if (expected != stream_.Finished()) {
    ADD_FAILURE()
        << (expected
                ? "The stream was expected to be finished, but it's not.\n"
                : "The stream was not expected to be finished, but it is.\n");
    EXPECT_EQ(expected, stream_.Finished());
    return false;
  }
  return true;
}

namespace {

unsigned DelimiterToSize(const unsigned char* delimiter) {
  unsigned size = 0;
  size = size | static_cast<unsigned>(delimiter[1]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[2]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[3]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[4]);
  return size;
}

}  // namespace

bool ProtoStreamTester::ValidateDelimiter(const std::string& message) {
  // First check the status of the stream
  if (!ExpectStatusEq(google::protobuf::util::error::OK)) {
    return false;
  }
  if (message.size() < kDelimiterSize) {
    ADD_FAILURE() << "ProtoStreamTester::ValidateSizeDelimiter: message size "
                     "is less than a delimiter size (5).\n";
  }
  int size_actual =
      DelimiterToSize(reinterpret_cast<const unsigned char*>(&message[0]));
  int size_expected = static_cast<int>(message.size() - kDelimiterSize);
  if (size_expected != size_actual) {
    ADD_FAILURE() << "The size extracted from the message delimiter: "
                  << size_actual
                  << " doesn't match the size of the message: " << size_expected
                  << std::endl;
    return false;
  }
  return true;
}

bool ProtoStreamTester::ExpectStatusEq(int error_code) {
  if (error_code != stream_.Status().error_code()) {
    ADD_FAILURE()
        << "ObjectTranslatorTest::ValidateStatus: Status doesn't match "
           "expected: "
        << error_code << " actual: " << stream_.Status().error_code() << " - "
        << stream_.Status().error_message() << std::endl;
    return false;
  }
  return true;
}

}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
