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

#include "src/signature.h"
#include "utils/md5.h"

#include <time.h>
#include "google/protobuf/text_format.h"
#include "gtest/gtest.h"

using std::string;
using ::google::protobuf::TextFormat;

namespace istio {
namespace mixer_client {
namespace {

class SignatureUtilTest : public ::testing::Test {
 protected:
  void AddString(const string& key, const string& value) {
    Attributes::Value string_value;
    string_value.type = Attributes::Value::STRING;
    string_value.str_v = value;
    attributes_.attributes[key] = string_value;
  }

  void AddBytes(const string& key, const string& value) {
    Attributes::Value bytes_value;
    bytes_value.type = Attributes::Value::BYTES;
    bytes_value.str_v = value;
    attributes_.attributes[key] = bytes_value;
  }

  void AddTime(
      const string& key,
      const std::chrono::time_point<std::chrono::system_clock>& value) {
    Attributes::Value time_value;
    time_value.type = Attributes::Value::TIME;
    time_value.time_v = value;
    attributes_.attributes[key] = time_value;
  }

  void AddDoublePair(const string& key, double value) {
    Attributes::Value double_value;
    double_value.type = Attributes::Value::DOUBLE;
    double_value.value.double_v = value;
    attributes_.attributes[key] = double_value;
  }

  void AddInt64Pair(const string& key, int64_t value) {
    Attributes::Value int64_value;
    int64_value.type = Attributes::Value::INT64;
    int64_value.value.int64_v = value;
    attributes_.attributes[key] = int64_value;
  }

  void AddBoolPair(const string& key, bool value) {
    Attributes::Value bool_value;
    bool_value.type = Attributes::Value::BOOL;
    bool_value.value.bool_v = value;
    attributes_.attributes[key] = bool_value;
  }

  void AddDuration(const string& key, std::chrono::nanoseconds value) {
    Attributes::Value a_value;
    a_value.type = Attributes::Value::DURATION;
    a_value.duration_nanos_v = value;
    attributes_.attributes[key] = a_value;
  }

  void AddStringMap(const string& key,
                    std::map<std::string, std::string>&& value) {
    Attributes::Value a_value;
    a_value.type = Attributes::Value::STRING_MAP;
    a_value.string_map_v.swap(value);
    attributes_.attributes[key] = a_value;
  }

  Attributes attributes_;
};

TEST_F(SignatureUtilTest, Attributes) {
  AddString("string-key", "this is a string value");
  EXPECT_EQ("26f8f724383c46e7f5803380ab9c17ba",
            MD5::DebugString(GenerateSignature(attributes_)));

  AddBytes("bytes-key", "this is a bytes value");
  EXPECT_EQ("1f409524b79b9b5760032dab7ecaf960",
            MD5::DebugString(GenerateSignature(attributes_)));

  AddDoublePair("double-key", 99.9);
  EXPECT_EQ("6183342ff222018f6300de51cdcd4501",
            MD5::DebugString(GenerateSignature(attributes_)));

  AddInt64Pair("int-key", 35);
  EXPECT_EQ("d681b9c72d648f9c831d95b4748fe1c2",
            MD5::DebugString(GenerateSignature(attributes_)));

  AddBoolPair("bool-key", true);
  EXPECT_EQ("958930b41f0d8b43f5c61c31b0b092e2",
            MD5::DebugString(GenerateSignature(attributes_)));

  // default to Clock's epoch.
  std::chrono::time_point<std::chrono::system_clock> time_point;
  AddTime("time-key", time_point);
  EXPECT_EQ("f7dd61e1a5881e2492d93ad023ab49a2",
            MD5::DebugString(GenerateSignature(attributes_)));

  std::chrono::seconds secs(5);
  AddDuration("duration-key",
              std::chrono::duration_cast<std::chrono::nanoseconds>(secs));
  EXPECT_EQ("13ae11ea2bea216da46688cd9698645e",
            MD5::DebugString(GenerateSignature(attributes_)));

  std::map<std::string, std::string> string_map = {{"key1", "value1"},
                                                   {"key2", "value2"}};
  AddStringMap("string-map-key", std::move(string_map));
  EXPECT_EQ("c861f02e251c896513eb0f7c97aa2ce7",
            MD5::DebugString(GenerateSignature(attributes_)));
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
