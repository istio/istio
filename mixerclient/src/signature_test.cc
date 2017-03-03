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
