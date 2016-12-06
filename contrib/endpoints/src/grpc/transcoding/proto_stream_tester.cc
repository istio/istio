// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
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
