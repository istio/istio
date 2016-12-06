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
#include "src/grpc/transcoding/message_stream.h"

#include <deque>
#include <string>

#include "gtest/gtest.h"
#include "src/grpc/transcoding/test_common.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {
namespace {

namespace pbio = ::google::protobuf::io;
namespace pbutil = ::google::protobuf::util;

// A test MessageStream implementation for testing ZeroCopyInputStream over
// MessageStream implementation.
class TestMessageStream : public MessageStream {
 public:
  TestMessageStream() : finished_(false) {}

  void AddMessage(std::string message) {
    messages_.emplace_back(std::move(message));
  }

  void Finish() { finished_ = true; }

  // MessageStream implementation
  bool NextMessage(std::string* message) {
    if (messages_.empty()) {
      return false;
    } else {
      *message = std::move(messages_.front());
      messages_.pop_front();
      return true;
    }
  }
  bool Finished() const { return messages_.empty() && finished_; }
  pbutil::Status Status() const { return pbutil::Status::OK; }

 private:
  bool finished_;
  std::deque<std::string> messages_;
};

class ZeroCopyInputStreamOverMessageStreamTest : public ::testing::Test {
 protected:
  ZeroCopyInputStreamOverMessageStreamTest() {}

  typedef std::vector<std::string> Messages;

  bool Test(const Messages& messages) {
    TestMessageStream test_message_stream;
    auto zero_copy_stream = test_message_stream.CreateZeroCopyInputStream();

    const void* data = nullptr;
    int size = 0;

    // Check that Next() returns true and a 0-sized buffer meaning that
    // nothing is available at the moment.
    if (!zero_copy_stream->Next(&data, &size)) {
      ADD_FAILURE() << "The stream finished unexpectedly" << std::endl;
      return false;
    }
    if (0 != size) {
      EXPECT_EQ(0, size);
      return false;
    }

    for (auto message : messages) {
      // Add the message to the MessageStream
      test_message_stream.AddMessage(message);

      // message.size() bytes must be available for reading
      if (static_cast<int>(message.size()) != zero_copy_stream->ByteCount()) {
        EXPECT_EQ(message.size(), zero_copy_stream->ByteCount());
        return false;
      }

      // Now try to read & match the message
      if (!zero_copy_stream->Next(&data, &size)) {
        ADD_FAILURE() << "The stream finished unexpectedly" << std::endl;
        return false;
      }
      auto actual = std::string(reinterpret_cast<const char*>(data), size);
      if (message != actual) {
        EXPECT_EQ(message, actual);
        return false;
      }

      // Try backing up & reading again different sizes
      auto backup_sizes = {1ul,
                           2ul,
                           10ul,
                           message.size() / 2ul,
                           3ul * message.size() / 4ul,
                           message.size()};

      for (auto backup_size : backup_sizes) {
        if (0 == backup_size || message.size() < backup_size) {
          // Not a valid test case
          continue;
        }
        zero_copy_stream->BackUp(backup_size);

        // backup_size bytes must be available for reading again
        if (static_cast<int>(backup_size) != zero_copy_stream->ByteCount()) {
          EXPECT_EQ(message.size(), zero_copy_stream->ByteCount());
          return false;
        }

        // Now Next() must return the backed up data again.
        if (!zero_copy_stream->Next(&data, &size)) {
          ADD_FAILURE() << "The stream finished unexpectedly" << std::endl;
          return false;
        }
        auto actual = std::string(reinterpret_cast<const char*>(data), size);
        // We expect the last backup_size bytes of the message.
        auto expected = message.substr(message.size() - backup_size);
        if (expected != actual) {
          EXPECT_EQ(expected, actual);
          return false;
        }
      }

      // At this point no data should be available
      if (!zero_copy_stream->Next(&data, &size)) {
        ADD_FAILURE() << "The stream finished unexpectedly" << std::endl;
        return false;
      }
      if (0 != size) {
        EXPECT_EQ(0, size);
        return false;
      }
    }

    // Now finish the MessageStream & make sure the ZeroCopyInputStream has
    // ended.
    test_message_stream.Finish();
    if (zero_copy_stream->Next(&data, &size)) {
      ADD_FAILURE() << "The stream still hasn't finished" << std::endl;
      return false;
    }

    return true;
  }
};

TEST_F(ZeroCopyInputStreamOverMessageStreamTest, OneMessage) {
  EXPECT_TRUE(Test(Messages{1, "This is a test message"}));
}

TEST_F(ZeroCopyInputStreamOverMessageStreamTest, ThreeMessages) {
  EXPECT_TRUE(Test(Messages{"Message One", "Message Two", "Message Three"}));
}

TEST_F(ZeroCopyInputStreamOverMessageStreamTest, TenKMessages) {
  Messages messages;
  for (int i = 1; i <= 10000; ++i) {
    messages.emplace_back("Message " + std::to_string(i));
  }
  EXPECT_TRUE(Test(messages));
}

TEST_F(ZeroCopyInputStreamOverMessageStreamTest, DifferenteSizes) {
  auto sizes = {0, 1, 2, 3, 4, 5, 6, 10, 12, 100, 128, 256, 1024, 4096, 65537};

  for (auto size : sizes) {
    EXPECT_TRUE(Test(Messages{1, GenerateInput("abcdefg12345", size)}));
  }
}

TEST_F(ZeroCopyInputStreamOverMessageStreamTest, DifferenteSizesOneStream) {
  auto sizes = {0, 1, 2, 3, 4, 5, 6, 10, 12, 100, 128, 256, 1024, 4096, 65537};

  Messages messages;
  for (auto size : sizes) {
    messages.emplace_back(GenerateInput("abcdefg12345", size));
  }
  EXPECT_TRUE(Test(messages));
}

TEST_F(ZeroCopyInputStreamOverMessageStreamTest, DirectTest) {
  TestMessageStream test_message_stream;
  auto zero_copy_stream = test_message_stream.CreateZeroCopyInputStream();

  const void* data = nullptr;
  int size = 0;

  // Check that Next() returns true and a 0-sized buffer meaning that
  // nothing is available at the moment.
  EXPECT_TRUE(zero_copy_stream->Next(&data, &size));
  EXPECT_EQ(0, size);

  // Test messages
  std::string message1 = "This is a message";
  std::string message2 = "This is a message too";
  std::string message3 = "Another message";
  std::string message4 = "Yet another message";

  // Add message1 to the MessageStream
  test_message_stream.AddMessage(message1);

  // message1 is available for reading
  EXPECT_EQ(message1.size(), zero_copy_stream->ByteCount());
  EXPECT_TRUE(zero_copy_stream->Next(&data, &size));
  EXPECT_EQ(message1, std::string(reinterpret_cast<const char*>(data), size));

  // Back up a bit
  zero_copy_stream->BackUp(5);

  // Now read the backed up data again
  EXPECT_EQ(5, zero_copy_stream->ByteCount());
  EXPECT_TRUE(zero_copy_stream->Next(&data, &size));
  EXPECT_EQ(message1.substr(message1.size() - 5),
            std::string(reinterpret_cast<const char*>(data), size));

  // Add message2 to the MessageStream
  test_message_stream.AddMessage(message2);

  // message2 is available for reading
  EXPECT_EQ(message2.size(), zero_copy_stream->ByteCount());
  EXPECT_TRUE(zero_copy_stream->Next(&data, &size));
  EXPECT_EQ(message2, std::string(reinterpret_cast<const char*>(data), size));

  // Back up all of message2
  zero_copy_stream->BackUp(message2.size());

  // Now read message2 again
  EXPECT_EQ(message2.size(), zero_copy_stream->ByteCount());
  EXPECT_TRUE(zero_copy_stream->Next(&data, &size));
  EXPECT_EQ(message2, std::string(reinterpret_cast<const char*>(data), size));

  // At this point no data should be available
  EXPECT_TRUE(zero_copy_stream->Next(&data, &size));
  EXPECT_EQ(0, size);

  // Add both message3 & message4 & finish the MessageStream afterwards
  test_message_stream.AddMessage(message3);
  test_message_stream.AddMessage(message4);
  test_message_stream.Finish();

  // Read & match both message3 & message4
  EXPECT_EQ(message3.size(), zero_copy_stream->ByteCount());
  EXPECT_TRUE(zero_copy_stream->Next(&data, &size));
  EXPECT_EQ(message3, std::string(reinterpret_cast<const char*>(data), size));

  EXPECT_EQ(message4.size(), zero_copy_stream->ByteCount());
  EXPECT_TRUE(zero_copy_stream->Next(&data, &size));
  EXPECT_EQ(message4, std::string(reinterpret_cast<const char*>(data), size));

  // All done!
  EXPECT_FALSE(zero_copy_stream->Next(&data, &size));
}

}  // namespace
}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
