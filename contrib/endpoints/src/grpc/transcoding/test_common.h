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
#ifndef GRPC_TRANSCODING_TEST_COMMON_H_
#define GRPC_TRANSCODING_TEST_COMMON_H_

#include <deque>
#include <functional>
#include <string>
#include <vector>

#include "google/api/service.pb.h"
#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/text_format.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {

// An implementation of ZeroCopyInputStream for testing.
// The tests define the chunks that TestZeroCopyInputStream produces.
class TestZeroCopyInputStream
    : public ::google::protobuf::io::ZeroCopyInputStream {
 public:
  TestZeroCopyInputStream();

  // Add an input chunk
  void AddChunk(std::string chunk);

  // Ends the stream
  void Finish() { finished_ = true; }

  // Returns whether Finish() has been called on this stream or not.
  bool Finished() const { return finished_; }

  // ZeroCopyInputStream methods
  bool Next(const void** data, int* size);
  void BackUp(int count);
  ::google::protobuf::int64 ByteCount() const;
  bool Skip(int) { return false; }  // Not implemented

 private:
  std::deque<std::string> chunks_;
  bool finished_;

  int position_;
  std::string current_;
};

// Test the translation test case with different partitions of the input. The
// test will generate combinations of partitioning input into specified number
// of chunks (chunk_count). For each of the input partition, test assertion is
// verified. The partition is passed to the test assertion as a std::vector of
// partitioning points in the input.
// Because the number of partitionings is O(N^chunk_count) we use a coefficient
// which controls which fraction of partitionings is generated and tested.
// The process of generating partitionings is deterministic.
//
// chunk_count - the number of parts (chunks) in each partition
// partitioning_coefficient - a real number in (0, 1] interval that defines how
//                            exhaustive the test should be, i.e. what part of
//                            all partitions of the input string should be
//                            tested (1.0 means all partitions).
// input - the input string
// test - the test to run
bool RunTestForInputPartitions(
    size_t chunk_count, double partitioning_coefficient,
    const std::string& input,
    std::function<bool(const std::vector<size_t>& t)> test);

// Generate an input string of the specified size using the specified seed.
std::string GenerateInput(const std::string& seed, size_t size);

// Load service from a proto text file. Returns true if loading succeeds;
// otherwise returns false.
bool LoadService(const std::string& config_pb_txt_file,
                 ::google::api::Service* service);

// Parses the gRPC message delimiter and returns the size of the message.
unsigned DelimiterToSize(const unsigned char* delimiter);

// Generates a gRPC message delimiter with the given message size.
std::string SizeToDelimiter(unsigned size);

// Genereate a proto message with the gRPC delimiter from proto text
template <class MessageType>
std::string GenerateGrpcMessage(const std::string& proto_text) {
  // Parse the message from text & serialize to binary
  MessageType message;
  EXPECT_TRUE(
      ::google::protobuf::TextFormat::ParseFromString(proto_text, &message));
  std::string binary;
  EXPECT_TRUE(message.SerializeToString(&binary));

  // Now prefix the binary with a delimiter and return
  return SizeToDelimiter(binary.size()) + binary;
}

// Compares JSON objects
bool ExpectJsonObjectEq(const std::string& expected, const std::string& actual);

// Compares JSON arrays
bool ExpectJsonArrayEq(const std::string& expected, const std::string& actual);

// JSON array tester that supports matching partial arrays.
class JsonArrayTester {
 public:
  // Tests a new element of the array.
  // expected - the expected new element of the array to match
  // actual - the actual JSON chunk (which will include "[", "]" or "," if
  //          needed)
  bool TestElement(const std::string& expected, const std::string& actual);

  // Tests a new chunk of the array (potentially multiple elements).
  // expected - the expected new chunk of the array to match (including "[", "]"
  //            or "," if needed)
  // actual - the actual JSON chunk (including "[", "]" or "," if needed)
  // closes - indicates whether the chunk closes the array or not.
  bool TestChunk(const std::string& expected, const std::string& actual,
                 bool closes);

  // Test that the array is closed after adding the given JSON chunk (i.e. must
  // be "]" modulo whitespace)
  bool TestClosed(const std::string& actual);

 private:
  std::string expected_so_far_;
  std::string actual_so_far_;
};

}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_MESSAGE_READER_H_
