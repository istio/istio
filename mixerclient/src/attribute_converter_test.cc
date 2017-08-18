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
words: "source user"
words: "string-key"
words: "this is a string value"
words: "string-map-key"
words: "key1"
words: "value1"
words: "key2"
words: "value2"
words: "time-key"
strings {
  key: -7
  value: -8
}
strings {
  key: 6
  value: -6
}
int64s {
  key: -5
  value: 35
}
int64s {
  key: 8
  value: 8080
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
  key: -14
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
  key: -9
  value {
    entries {
      key: -12
      value: -13
    }
    entries {
      key: -10
      value: -11
    }
  }
}
)";

const char kReportAttributes[] = R"(
attributes {
  strings {
    key: -7
    value: -8
  }
  strings {
    key: 6
    value: -6
  }
  int64s {
    key: -5
    value: 35
  }
  int64s {
    key: 8
    value: 8080
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
    key: -14
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
    key: -9
    value {
      entries {
        key: -12
        value: -13
      }
      entries {
        key: -10
        value: -11
      }
    }
  }
}
attributes {
  int64s {
    key: -15
    value: 111
  }
  int64s {
    key: -5
    value: 135
  }
  doubles {
    key: -3
    value: 123.99
  }
  bools {
    key: -1
    value: false
  }
  string_maps {
    key: -9
    value {
      entries {
        key: -16
        value: -17
      }
      entries {
        key: 32
        value: 90
      }
    }
  }
}
default_words: "bool-key"
default_words: "bytes-key"
default_words: "double-key"
default_words: "duration-key"
default_words: "int-key"
default_words: "source user"
default_words: "string-key"
default_words: "this is a string value"
default_words: "string-map-key"
default_words: "key1"
default_words: "value1"
default_words: "key2"
default_words: "value2"
default_words: "time-key"
default_words: "int-key2"
default_words: "key"
default_words: "value"
global_word_count: 150
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

  void SetUp() {
    AddString("string-key", "this is a string value");
    AddBytes("bytes-key", "this is a bytes value");
    AddDoublePair("double-key", 99.9);
    AddInt64Pair("int-key", 35);
    AddBoolPair("bool-key", true);

    // add some global words
    AddString("source.user", "source user");
    AddInt64Pair("target.port", 8080);

    // default to Clock's epoch.
    std::chrono::time_point<std::chrono::system_clock> time_point;
    AddTime("time-key", time_point);

    std::chrono::seconds secs(5);
    AddDuration("duration-key",
                std::chrono::duration_cast<std::chrono::nanoseconds>(secs));

    std::map<std::string, std::string> string_map = {{"key1", "value1"},
                                                     {"key2", "value2"}};
    AddStringMap("string-map-key", std::move(string_map));
  }

  Attributes attributes_;
};

TEST_F(AttributeConverterTest, ConvertTest) {
  // A converter with an empty global dictionary.
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

TEST_F(AttributeConverterTest, BatchConvertTest) {
  // A converter with an empty global dictionary.
  AttributeConverter converter({});
  auto batch_converter = converter.CreateBatchConverter();

  EXPECT_TRUE(batch_converter->Add(attributes_));

  // modify some attributes
  AddDoublePair("double-key", 123.99);
  AddInt64Pair("int-key", 135);
  AddInt64Pair("int-key2", 111);
  AddBoolPair("bool-key", false);

  AddStringMap("string-map-key", {{"key", "value"}, {":method", "GET"}});

  // Since there is no deletion, batch is good
  EXPECT_TRUE(batch_converter->Add(attributes_));

  // remove a key
  attributes_.attributes.erase("int-key2");
  // Batch should fail.
  EXPECT_FALSE(batch_converter->Add(attributes_));

  auto report_pb = batch_converter->Finish();

  std::string out_str;
  TextFormat::PrintToString(*report_pb, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::ReportRequest expected_report_pb;
  ASSERT_TRUE(
      TextFormat::ParseFromString(kReportAttributes, &expected_report_pb));
  EXPECT_TRUE(MessageDifferencer::Equals(*report_pb, expected_report_pb));
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
