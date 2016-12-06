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
#include "src/grpc/transcoding/message_reader.h"

#include <memory>
#include <sstream>
#include <string>
#include <vector>

#include "gtest/gtest.h"
#include "src/grpc/transcoding/test_common.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {
namespace {

std::string ReadAllFromStream(
    ::google::protobuf::io::ZeroCopyInputStream* stream) {
  std::ostringstream all;
  const void* data = nullptr;
  int size = 0;
  while (stream->Next(&data, &size) && size != 0) {
    all << std::string(static_cast<const char*>(data), size);
  }
  return all.str();
}

// A helper structure to store a single expected message and its position
struct ExpectedAt {
  // The position in the input, after which this message is expected
  size_t at;
  std::string message;
};

// MessageReaderTestRun tests a single MessageReader processing the input as
// expected.
// It allows feeding chunks of the input (AddChunk()) to the MessageReader and
// testing that the MessageReader yields the expected messages at any point
// (Test()).
class MessageReaderTestRun {
 public:
  // input - the input to be passed to the MessageReader
  // expected - the expected messages as the input is processed
  // NOTE: Both input and expected are stored as references in the
  //       MessageReaderTestRun, so the caller must make sure they exist
  //       throughout the lifetime of MessageReaderTestRun.
  MessageReaderTestRun(const std::string& input,
                       const std::vector<ExpectedAt>& expected)
      : input_(input),
        expected_(expected),
        input_stream_(new TestZeroCopyInputStream()),
        reader_(new MessageReader(input_stream_.get())),
        position_(0),
        next_expected_(std::begin(expected_)) {}

  // Returns the total size of the input including the message delimiters
  size_t TotalInputSize() const { return input_.size(); }

  // Adds a chunk of a given size to be processed
  void AddChunk(size_t size) {
    input_stream_->AddChunk(input_.substr(position_, size));
    position_ += size;
  }

  // Finishes the input stream
  void FinishInputStream() { input_stream_->Finish(); }

  // Tests the MessageReader at the current position of the input.
  bool Test() {
    // While we still have expected messages before or at the current position
    // try to match.
    while (next_expected_ != std::end(expected_) &&
           next_expected_->at <= position_) {
      // Must not be finished as we expect a message
      if (reader_->Finished()) {
        ADD_FAILURE() << "Finished unexpectedly" << std::endl;
        return false;
      }
      // Read the message
      auto stream = reader_->NextMessage();
      if (!stream) {
        ADD_FAILURE() << "No message available" << std::endl;
        return false;
      }
      // Match the message with the expected message
      auto message = ReadAllFromStream(stream.get());
      if (next_expected_->message != message) {
        EXPECT_EQ(next_expected_->message, message);
        return false;
      }
      // Move to the next expected message
      ++next_expected_;
    }
    // We have read all the expected messages, so NextMessage() must return
    // nullptr
    if (reader_->NextMessage().get()) {
      ADD_FAILURE() << "Unexpected message" << std::endl;
      return false;
    }
    // Expect the reader to be finished iff we have called Finish() on
    // input_stream_.
    if (input_stream_->Finished() != reader_->Finished()) {
      EXPECT_EQ(input_stream_->Finished(), reader_->Finished());
      return false;
    }
    return true;
  }

 private:
  const std::string& input_;
  const std::vector<ExpectedAt>& expected_;

  std::unique_ptr<TestZeroCopyInputStream> input_stream_;
  std::unique_ptr<MessageReader> reader_;

  // The current position in the input stream.
  size_t position_;

  // An iterator that points the next expected message.
  std::vector<ExpectedAt>::const_iterator next_expected_;
};

// MessageReaderTestCase tests a single input test case with different
// partitions of the input.
class MessageReaderTestCase {
 public:
  MessageReaderTestCase(std::vector<std::string> messages) {
    for (const auto& message : messages) {
      // First add the delimiter
      input_ += SizeToDelimiter(message.size());
      // Then add the message itself
      input_ += message;
      // Remember that we should expect this message after input_.size() bytes
      // are processed.
      expected_.emplace_back(ExpectedAt{input_.size(), message});
    }
  }

  std::unique_ptr<MessageReaderTestRun> NewRun() {
    return std::unique_ptr<MessageReaderTestRun>(
        new MessageReaderTestRun(input_, expected_));
  }

  // Runs the test for different partitions of the input.
  // chunk_count - the number of chunks (parts) per partition
  // partitioning_coefficient - defines how exhaustive the test should be. See
  //                            the comment on RunTestForInputPartitions() in
  //                            test_common.h for more details.
  bool Test(size_t chunk_count, double partitioning_coefficient) {
    return RunTestForInputPartitions(chunk_count, partitioning_coefficient,
                                     input_,
                                     [this](const std::vector<size_t>& t) {
                                       auto run = NewRun();

                                       // Feed the chunks according to the
                                       // partition defined by tuple t and
                                       // test along the way.
                                       size_t pos = 0;
                                       for (size_t i = 0; i < t.size(); ++i) {
                                         run->AddChunk(t[i] - pos);
                                         pos = t[i];
                                         if (!run->Test()) {
                                           return false;
                                         }
                                       }
                                       // Feed the last chunk, finish & test.
                                       run->AddChunk(input_.size() - pos);
                                       run->FinishInputStream();
                                       return run->Test();
                                     });
  }

 private:
  std::string input_;
  std::vector<ExpectedAt> expected_;
};

class MessageReaderTest : public ::testing::Test {
 protected:
  MessageReaderTest() {}

  // Add an input message
  void AddMessage(std::string message) {
    messages_.emplace_back(std::move(message));
  }

  // Builds a test case using the added messages and clears the messages in case
  // the test needs to add messages & build another test case.
  std::unique_ptr<MessageReaderTestCase> Build() {
    std::vector<std::string> messages;
    messages.swap(messages_);
    return std::unique_ptr<MessageReaderTestCase>(
        new MessageReaderTestCase(std::move(messages)));
  }

 private:
  std::vector<std::string> messages_;
};

TEST_F(MessageReaderTest, OneMessage) {
  AddMessage("SimpleMessage");

  auto tc = Build();
  EXPECT_TRUE(tc->Test(1, 1.0));
  EXPECT_TRUE(tc->Test(2, 1.0));
  EXPECT_TRUE(tc->Test(3, 1.0));
  EXPECT_TRUE(tc->Test(4, 0.1));
}

TEST_F(MessageReaderTest, ThreeMessages) {
  AddMessage("Message1");
  AddMessage("Message2");
  AddMessage("Message3");

  auto tc = Build();
  EXPECT_TRUE(tc->Test(1, 1.0));
  EXPECT_TRUE(tc->Test(2, 1.0));
  EXPECT_TRUE(tc->Test(3, 1.0));
  EXPECT_TRUE(tc->Test(4, 0.2));
}

TEST_F(MessageReaderTest, TenKMessages) {
  for (size_t i = 1; i < 10000; ++i) {
    AddMessage("Message" + std::to_string(i));
  }
  auto tc = Build();
  EXPECT_TRUE(tc->Test(1, 1.0));
}

TEST_F(MessageReaderTest, DifferentSizesAllInOneStream) {
  auto sizes = {0, 1, 2, 3, 4, 5, 6, 10, 12, 100, 128, 256, 1024, 4096, 65537};

  for (auto size : sizes) {
    AddMessage(GenerateInput("abcdefg", size));
  }
  auto tc = Build();
  EXPECT_TRUE(tc->Test(1, 1.0));
}

TEST_F(MessageReaderTest, DifferentSizesEachInItsOwnStream) {
  auto sizes = {0, 1, 2, 3, 4, 5, 6, 10, 12, 100, 128, 256, 1024, 4096, 65537};

  for (auto size : sizes) {
    AddMessage(GenerateInput("abcdefg", size));
    auto tc = Build();
    EXPECT_TRUE(tc->Test(1, 1.0));
    if (size < 50) {
      EXPECT_TRUE(tc->Test(2, 1.0));
      EXPECT_TRUE(tc->Test(3, 1.0));
    }
  }
}

TEST_F(MessageReaderTest, OneByteChunks) {
  AddMessage("Message1");
  AddMessage("Message2");
  AddMessage("Message3");

  auto tc = Build();
  auto tr = tc->NewRun();

  for (size_t i = 0; i < tr->TotalInputSize(); ++i) {
    tr->AddChunk(1);
    EXPECT_TRUE(tr->Test());
  }
  tr->FinishInputStream();
  EXPECT_TRUE(tr->Test());
}

TEST_F(MessageReaderTest, DirectTest) {
  TestZeroCopyInputStream input_stream;
  MessageReader reader(&input_stream);

  std::string message1 = "This is a message";
  std::string message2 = "This is a different message";
  std::string message3 = "This is another message";
  std::string message4 = "This is yet another message";

  // Nothing yet
  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_FALSE(reader.Finished());

  // Add the delimiter for the first message
  input_stream.AddChunk(SizeToDelimiter(message1.size()));

  // Still nothing
  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_FALSE(reader.Finished());

  // Add the message itself
  input_stream.AddChunk(message1);

  // Now message1 must be available
  auto message1_stream = reader.NextMessage();
  ASSERT_NE(nullptr, message1_stream.get());
  EXPECT_EQ(message1, ReadAllFromStream(message1_stream.get()));
  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_FALSE(reader.Finished());

  // Add part of the message2
  input_stream.AddChunk(SizeToDelimiter(message2.size()));
  input_stream.AddChunk(message2.substr(0, 10));

  // No message should be available
  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_FALSE(reader.Finished());

  // Add the rest of the second message
  input_stream.AddChunk(message2.substr(10));

  // Now the message2 is available
  auto message2_stream = reader.NextMessage();
  ASSERT_NE(nullptr, message2_stream.get());
  EXPECT_EQ(message2, ReadAllFromStream(message2_stream.get()));
  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_FALSE(reader.Finished());

  // Adding both message3 & message4 in one shot and Finish the stream
  input_stream.AddChunk(SizeToDelimiter(message3.size()));
  input_stream.AddChunk(message3);
  input_stream.AddChunk(SizeToDelimiter(message4.size()));
  input_stream.AddChunk(message4);
  input_stream.Finish();

  // Not finished yet as we still have messages to read
  EXPECT_FALSE(reader.Finished());

  // Now both message3 & message4 must be available
  auto message3_stream = reader.NextMessage();
  ASSERT_NE(nullptr, message3_stream.get());
  EXPECT_EQ(message3, ReadAllFromStream(message3_stream.get()));
  EXPECT_FALSE(reader.Finished());

  auto message4_stream = reader.NextMessage();
  ASSERT_NE(nullptr, message4_stream.get());
  EXPECT_EQ(message4, ReadAllFromStream(message4_stream.get()));

  // All done, the reader must be finished
  EXPECT_EQ(nullptr, reader.NextMessage().get());
  EXPECT_TRUE(reader.Finished());
}

}  // namespace
}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
