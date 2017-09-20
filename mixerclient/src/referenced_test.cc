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

#include "src/referenced.h"
#include "utils/md5.h"

#include "google/protobuf/text_format.h"
#include "gtest/gtest.h"

using ::google::protobuf::TextFormat;

namespace istio {
namespace mixer_client {
namespace {

const char kReferencedText[] = R"(
words: "bool-key"
words: "bytes-key"
words: "string-key"
words: "double-key"
words: "int-key"
words: "time-key"
words: "duration-key"
words: "string-map-key"
attribute_matches {
  name: 9,
  condition: ABSENCE,
}
attribute_matches {
  name: 10,
  condition: ABSENCE,
}
attribute_matches {
  name: -1,
  condition: EXACT,
}
attribute_matches {
  name: -2,
  condition: EXACT,
}
attribute_matches {
  name: -3,
  condition: EXACT,
}
attribute_matches {
  name: -4,
  condition: EXACT,
}
attribute_matches {
  name: -5,
  condition: EXACT,
}
attribute_matches {
  name: -6,
  condition: EXACT,
}
attribute_matches {
  name: -7,
  condition: EXACT,
}
attribute_matches {
  name: -8,
  condition: EXACT,
}
)";

// Global index (positive) is too big
const char kReferencedFailText1[] = R"(
attribute_matches {
  name: 10000,
  condition: EXACT,
}
)";

// Per message index (negative) is too big
const char kReferencedFailText2[] = R"(
words: "bool-key"
words: "bytes-key"
attribute_matches {
  name: -10,
  condition: ABSENCE,
}
)";

TEST(ReferencedTest, FillSuccessTest) {
  ::istio::mixer::v1::ReferencedAttributes pb;
  ASSERT_TRUE(TextFormat::ParseFromString(kReferencedText, &pb));

  Referenced referenced;
  EXPECT_TRUE(referenced.Fill(pb));

  EXPECT_EQ(referenced.DebugString(),
            "Absence-keys: target.service, target.name, Exact-keys: bool-key, "
            "bytes-key, string-key, double-key, int-key, time-key, "
            "duration-key, string-map-key, ");

  EXPECT_EQ(MD5::DebugString(referenced.Hash()),
            "e8066212fef8b8a2fde6daf0f7974d01");
}

TEST(ReferencedTest, FillFail1Test) {
  ::istio::mixer::v1::ReferencedAttributes pb;
  ASSERT_TRUE(TextFormat::ParseFromString(kReferencedFailText1, &pb));

  Referenced referenced;
  EXPECT_FALSE(referenced.Fill(pb));
}

TEST(ReferencedTest, FillFail2Test) {
  ::istio::mixer::v1::ReferencedAttributes pb;
  ASSERT_TRUE(TextFormat::ParseFromString(kReferencedFailText2, &pb));

  Referenced referenced;
  EXPECT_FALSE(referenced.Fill(pb));
}

TEST(ReferencedTest, NegativeSignature1Test) {
  ::istio::mixer::v1::ReferencedAttributes pb;
  ASSERT_TRUE(TextFormat::ParseFromString(kReferencedText, &pb));
  Referenced referenced;
  EXPECT_TRUE(referenced.Fill(pb));

  std::string signature;

  Attributes attributes1;
  // "target.service" should be absence.
  attributes1.attributes["target.service"] = Attributes::StringValue("foo");
  EXPECT_FALSE(referenced.Signature(attributes1, "", &signature));

  Attributes attributes2;
  // many keys should exist.
  attributes2.attributes["bytes-key"] = Attributes::StringValue("foo");
  EXPECT_FALSE(referenced.Signature(attributes2, "", &signature));
}

TEST(ReferencedTest, OKSignature1Test) {
  ::istio::mixer::v1::ReferencedAttributes pb;
  ASSERT_TRUE(TextFormat::ParseFromString(kReferencedText, &pb));
  Referenced referenced;
  EXPECT_TRUE(referenced.Fill(pb));

  Attributes attributes;
  attributes.attributes["string-key"] =
      Attributes::StringValue("this is a string value");
  attributes.attributes["bytes-key"] =
      Attributes::BytesValue("this is a bytes value");
  attributes.attributes["double-key"] = Attributes::DoubleValue(99.9);
  attributes.attributes["int-key"] = Attributes::Int64Value(35);
  attributes.attributes["bool-key"] = Attributes::BoolValue(true);

  std::chrono::time_point<std::chrono::system_clock> time0;
  attributes.attributes["time-key"] = Attributes::TimeValue(time0);
  std::chrono::seconds secs(5);
  attributes.attributes["duration-key"] = Attributes::DurationValue(
      std::chrono::duration_cast<std::chrono::nanoseconds>(secs));

  std::map<std::string, std::string> string_map = {{"key1", "value1"},
                                                   {"key2", "value2"}};
  attributes.attributes["string-map-key"] =
      Attributes::StringMapValue(std::move(string_map));

  std::string signature;
  EXPECT_TRUE(referenced.Signature(attributes, "extra", &signature));

  EXPECT_EQ(MD5::DebugString(signature), "cfcf5f20f58a44da8832456742bf7b88");
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
