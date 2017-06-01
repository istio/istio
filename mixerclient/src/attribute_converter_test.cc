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

#include "src/attribute_converter.h"

#include <time.h>
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"

using std::string;
using ::google::protobuf::TextFormat;
using ::google::protobuf::util::MessageDifferencer;

namespace istio {
namespace mixer_client {
namespace {

const char kAttributes[] = R"(
words: "bool-key"
words: "bytes-key"
words: "double-key"
words: "duration-key"
words: "int-key"
words: "string-key"
words: "this is a string value"
words: "string-map-key"
words: "key1"
words: "value1"
words: "key2"
words: "value2"
words: "time-key"
strings {
  key: -6
  value: -7
}
int64s {
  key: -5
  value: 35
}
doubles {
  key: -3
  value: 99.9
}
bools {
  key: -1
  value: true
}
timestamps {
  key: -13
  value {
  }
}
durations {
  key: -4
  value {
    seconds: 5
  }
}
bytes {
  key: -2
  value: "this is a bytes value"
}
string_maps {
  key: -8
  value {
    entries {
      key: -11
      value: -12
    }
    entries {
      key: -9
      value: -10
    }
  }
}
)";

class AttributeConverterTest : public ::testing::Test {
 protected:
  void AddString(const string& key, const string& value) {
    attributes_.attributes[key] = Attributes::StringValue(value);
  }

  void AddBytes(const string& key, const string& value) {
    attributes_.attributes[key] = Attributes::BytesValue(value);
  }

  void AddTime(
      const string& key,
      const std::chrono::time_point<std::chrono::system_clock>& value) {
    attributes_.attributes[key] = Attributes::TimeValue(value);
  }

  void AddDoublePair(const string& key, double value) {
    attributes_.attributes[key] = Attributes::DoubleValue(value);
  }

  void AddInt64Pair(const string& key, int64_t value) {
    attributes_.attributes[key] = Attributes::Int64Value(value);
  }

  void AddBoolPair(const string& key, bool value) {
    attributes_.attributes[key] = Attributes::BoolValue(value);
  }

  void AddDuration(const string& key, std::chrono::nanoseconds value) {
    attributes_.attributes[key] = Attributes::DurationValue(value);
  }

  void AddStringMap(const string& key,
                    std::map<std::string, std::string>&& value) {
    attributes_.attributes[key] = Attributes::StringMapValue(std::move(value));
  }

  Attributes attributes_;
};

TEST_F(AttributeConverterTest, ConvertTest) {
  AddString("string-key", "this is a string value");
  AddBytes("bytes-key", "this is a bytes value");
  AddDoublePair("double-key", 99.9);
  AddInt64Pair("int-key", 35);
  AddBoolPair("bool-key", true);

  // default to Clock's epoch.
  std::chrono::time_point<std::chrono::system_clock> time_point;
  AddTime("time-key", time_point);

  std::chrono::seconds secs(5);
  AddDuration("duration-key",
              std::chrono::duration_cast<std::chrono::nanoseconds>(secs));

  std::map<std::string, std::string> string_map = {{"key1", "value1"},
                                                   {"key2", "value2"}};
  AddStringMap("string-map-key", std::move(string_map));

  AttributeConverter converter({});
  ::istio::mixer::v1::Attributes attributes_pb;
  converter.Convert(attributes_, &attributes_pb);

  std::string out_str;
  TextFormat::PrintToString(attributes_pb, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::Attributes expected_attributes_pb;
  ASSERT_TRUE(
      TextFormat::ParseFromString(kAttributes, &expected_attributes_pb));
  EXPECT_TRUE(
      MessageDifferencer::Equals(attributes_pb, expected_attributes_pb));
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
