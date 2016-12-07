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
#include "test_common.h"

#include <ctype.h>
#include <fstream>
#include <string>
#include <vector>

#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/struct.pb.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/json_util.h"
#include "google/protobuf/util/message_differencer.h"
#include "google/protobuf/util/type_resolver_util.h"
#include "google/protobuf/util/type_resolver_util.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {

namespace pb = ::google::protobuf;
namespace pbutil = ::google::protobuf::util;

TestZeroCopyInputStream::TestZeroCopyInputStream()
    : finished_(false), position_(0) {}

void TestZeroCopyInputStream::AddChunk(std::string chunk) {
  // Some of our tests generate 0-sized chunks. Let's not add them to the flow
  // here because the tests explicitly handle 'out of input' and 'there is now
  // new input' situations explicitly and empty chunks in the input would
  // interfere with the test driver logic.
  if (!chunk.empty()) {
    chunks_.emplace_back(std::move(chunk));
  }
}

bool TestZeroCopyInputStream::Next(const void** data, int* size) {
  if (position_ < static_cast<int>(current_.size())) {
    // Still have some data in the current buffer
    *data = current_.data() + position_;
    *size = current_.size() - position_;
    position_ = current_.size();
    return true;
  } else if (!chunks_.empty()) {
    // Return the next buffer
    current_ = std::move(chunks_.front());
    chunks_.pop_front();
    *data = current_.data();
    *size = current_.size();
    position_ = current_.size();
    return true;
  } else {
    *size = 0;
    return !finished_;
  }
}

void TestZeroCopyInputStream::BackUp(int count) {
  EXPECT_TRUE(count <= position_) << "Out of bounds while trying to back up "
                                     "the input stream. position: "
                                  << position_ << " count: " << count
                                  << std::endl;
  position_ -= count;
}

pb::int64 TestZeroCopyInputStream::ByteCount() const {
  auto total = current_.size() - position_;
  for (auto chunk : chunks_) {
    total += chunk.size();
  }
  return total;
}

namespace {

typedef std::vector<size_t> Tuple;

// Generates all distinct m-tuples of {1..n} numbers and calls f on each one.
// If f returns false, the enumeration stops; otherwise it continues.
// The numbers within each generated tuple are in increasing order.
bool ForAllDistinctTuples(size_t m, size_t n,
                          std::function<bool(const Tuple&)> f, Tuple& t) {
  if (0 == m) {
    // The base case
    return f(t);
  }
  for (unsigned i = (t.empty() ? 1 : t.back() + 1); i <= n; ++i) {
    t.emplace_back(i);
    if (!ForAllDistinctTuples(m - 1, n, f, t)) {
      // Somewhere f returned false - early termination
      return false;
    }
    t.pop_back();
  }
  return true;
}

// Convenience overload without the initial tuple
bool ForAllDistinctTuples(size_t m, size_t n,
                          std::function<bool(const Tuple&)> f) {
  Tuple t;
  return ForAllDistinctTuples(m, n, f, t);
}

// Calls f for partitioning_coefficient^m-th part of all the partitions (0 <
// partitioning_coefficient <= 1). It calls ForAllDistinctTuples() for
// partitioning_coefficient*n and multiplies each tuple by
// 1/partitioning_coefficient to achieve uniformity (more or less).
bool ForSomeDistinctTuples(size_t m, size_t n, double partitioning_coefficient,
                           std::function<bool(const Tuple&)> f) {
  return ForAllDistinctTuples(
      m, static_cast<size_t>(partitioning_coefficient * n),
      [f, partitioning_coefficient](const Tuple& t) {
        auto tt = t;
        for (auto& i : tt) {
          i = static_cast<size_t>(i / partitioning_coefficient);
        }
        return f(tt);
      });
}

// Returns a display string for a partition of str defined by tuple t. Used for
// displaying errors.
std::string PartitionToDisplayString(const std::string& str, const Tuple& t) {
  std::string result;
  size_t pos = 0;
  for (size_t i = 0; i < t.size(); ++i) {
    if (i > 0) {
      result += "  |  ";
    }
    result += str.substr(pos, t[i] - pos);
    pos = t[i];
  }
  result += "  |  ";
  result += str.substr(pos);
  return result;
}

}  // namespace

bool RunTestForInputPartitions(size_t chunk_count,
                               double partitioning_coefficient,
                               const std::string& input,
                               std::function<bool(const Tuple& t)> test) {
  // To choose a m-partition of input of size n, we need to choose m-1 breakdown
  // points between 1 and n-1.
  return ForSomeDistinctTuples(
      chunk_count - 1, input.size() - 1, partitioning_coefficient,
      [&input, test](const Tuple& t) {
        if (!test(t)) {
          ADD_FAILURE() << "Failed for the following partition \""
                        << PartitionToDisplayString(input, t) << "\"\n";
          return false;
        } else {
          return true;
        }
      });
}

std::string GenerateInput(const std::string& seed, size_t size) {
  std::string result = seed;
  while (result.size() < size) {
    result += seed;
  }
  result.resize(size);
  return result;
}

namespace {

std::string LoadFile(const std::string& input_file_name) {
  const char kTestdata[] = "contrib/endpoints/src/grpc/transcoding/testdata/";
  std::string file_name = std::string(kTestdata) + input_file_name;

  std::ifstream ifs(file_name);
  if (!ifs) {
    ADD_FAILURE() << "Could not open " << file_name.c_str() << std::endl;
    return std::string();
  }
  std::ostringstream ss;
  ss << ifs.rdbuf();
  return ss.str();
}

}  // namespace

bool LoadService(const std::string& config_pb_txt_file,
                 ::google::api::Service* service) {
  auto config = LoadFile(config_pb_txt_file);
  if (config.empty()) {
    return false;
  }

  if (!pb::TextFormat::ParseFromString(config, service)) {
    ADD_FAILURE() << "Could not parse service config from "
                  << config_pb_txt_file.c_str() << std::endl;
    return false;
  } else {
    return true;
  }
}

unsigned DelimiterToSize(const unsigned char* delimiter) {
  unsigned size = 0;
  // Bytes 1-4 are big-endian 32-bit message size
  size = size | static_cast<unsigned>(delimiter[1]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[2]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[3]);
  size <<= 8;
  size = size | static_cast<unsigned>(delimiter[4]);
  return size;
}

std::string SizeToDelimiter(unsigned size) {
  unsigned char delimiter[5];
  // Byte 0 is the compression bit - set to 0 (no compression)
  delimiter[0] = 0;
  // Bytes 1-4 are big-endian 32-bit message size
  delimiter[4] = 0xFF & size;
  size >>= 8;
  delimiter[3] = 0xFF & size;
  size >>= 8;
  delimiter[2] = 0xFF & size;
  size >>= 8;
  delimiter[1] = 0xFF & size;

  return std::string(reinterpret_cast<const char*>(delimiter),
                     sizeof(delimiter));
}

namespace {

bool JsonToStruct(const std::string& json, pb::Struct* message) {
  static std::unique_ptr<pbutil::TypeResolver> type_resolver(
      pbutil::NewTypeResolverForDescriptorPool(
          "type.googleapis.com", pb::Struct::descriptor()->file()->pool()));

  std::string binary;
  auto status = pbutil::JsonToBinaryString(
      type_resolver.get(), "type.googleapis.com/google.protobuf.Struct", json,
      &binary);
  if (!status.ok()) {
    ADD_FAILURE() << "Error: " << status.error_message() << std::endl
                  << "Failed to parse \"" << json << "\"." << std::endl;
    return false;
  }

  if (!message->ParseFromString(binary)) {
    ADD_FAILURE() << "Failed to create a struct message for \"" << json << "\"."
                  << std::endl;
    return false;
  }

  return true;
}

}  // namespace

bool ExpectJsonObjectEq(const std::string& expected_json,
                        const std::string& actual_json) {
  // Convert expected_json and actual_json to google.protobuf.Struct messages
  pb::Struct expected_proto, actual_proto;
  if (!JsonToStruct(expected_json, &expected_proto) ||
      !JsonToStruct(actual_json, &actual_proto)) {
    return false;
  }

  // Now try matching the protobuf messages
  if (!pbutil::MessageDifferencer::Equivalent(expected_proto, actual_proto)) {
    // Use EXPECT_EQ on debug strings to output the diff
    EXPECT_EQ(expected_proto.DebugString(), actual_proto.DebugString());
    return false;
  }

  return true;
}

bool ExpectJsonArrayEq(const std::string& expected, const std::string& actual) {
  // Wrap the JSON arrays into JSON objects and compare as object
  return ExpectJsonObjectEq(R"( { "array" : )" + expected + "}",
                            R"( { "array" : )" + actual + "}");
}

bool JsonArrayTester::TestElement(const std::string& expected,
                                  const std::string& actual) {
  if (expected_so_far_.empty()) {
    // First element - open the array and add the element
    expected_so_far_ = "[" + expected;
  } else {
    // Had elements before - add a comma and then the element
    expected_so_far_ += "," + expected;
  }
  actual_so_far_ += actual;

  // Add the closing "]" to the partial arrays and compare
  return ExpectJsonArrayEq(expected_so_far_ + "]", actual_so_far_ + "]");
}

bool JsonArrayTester::TestChunk(const std::string& expected,
                                const std::string& actual, bool closes) {
  expected_so_far_ += expected;
  actual_so_far_ += actual;

  // Add the closing "]" to the partial arrays if needed and compare
  return ExpectJsonArrayEq(expected_so_far_ + (closes ? "" : "]"),
                           actual_so_far_ + (closes ? "" : "]"));
}

bool JsonArrayTester::TestClosed(const std::string& actual) {
  if (expected_so_far_.empty()) {
    // Empty array case
    expected_so_far_ = "[";
  }
  // Close the finish
  expected_so_far_ += "]";
  actual_so_far_ += actual;

  // Compare the closed arrays
  return ExpectJsonArrayEq(expected_so_far_, actual_so_far_);
}

}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
