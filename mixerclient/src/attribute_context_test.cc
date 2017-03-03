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

#include "src/attribute_context.h"
#include "google/protobuf/text_format.h"
#include "google/protobuf/util/message_differencer.h"
#include "gtest/gtest.h"
#include "mixer/v1/service.pb.h"

using ::google::protobuf::util::MessageDifferencer;
using ::google::protobuf::TextFormat;

namespace istio {
namespace mixer_client {

// A expected full attribute proto in text.
const char kAttributeText[] = R"(
dictionary {
  key: 1
  value: "key1"
}
dictionary {
  key: 2
  value: "key11"
}
dictionary {
  key: 3
  value: "key2"
}
dictionary {
  key: 4
  value: "key21"
}
dictionary {
  key: 5
  value: "key3"
}
dictionary {
  key: 6
  value: "key31"
}
dictionary {
  key: 7
  value: "key4"
}
dictionary {
  key: 8
  value: "key41"
}
dictionary {
  key: 9
  value: "key5"
}
dictionary {
  key: 10
  value: "key51"
}
dictionary {
  key: 11
  value: "key6"
}
dictionary {
  key: 12
  value: "key61"
}
dictionary {
  key: 13
  value: "key7"
}
dictionary {
  key: 14
  value: "key71"
}
dictionary {
  key: 15
  value: "key8"
}
dictionary {
  key: 16
  value: "key81"
}
string_attributes {
  key: 1
  value: "string1"
}
string_attributes {
  key: 2
  value: "string2"
}
int64_attributes {
  key: 5
  value: 100
}
int64_attributes {
  key: 6
  value: 200
}
double_attributes {
  key: 7
  value: 1
}
double_attributes {
  key: 8
  value: 100
}
bool_attributes {
  key: 9
  value: true
}
bool_attributes {
  key: 10
  value: false
}
timestamp_attributes {
  key: 11
  value {
  }
}
timestamp_attributes {
  key: 12
  value {
    seconds: 1
  }
}
duration_attributes {
  key: 13
  value {
    seconds: 1
  }
}
duration_attributes {
  key: 14
  value {
    seconds: 2
  }
}
bytes_attributes {
  key: 3
  value: "bytes1"
}
bytes_attributes {
  key: 4
  value: "bytes2"
}
stringMap_attributes {
  key: 15
  value {
    map {
      key: 1
      value: "value1"
    }
  }
}
stringMap_attributes {
  key: 16
  value {
    map {
      key: 1
      value: "value1"
    }
    map {
      key: 3
      value: "value2"
    }
  }
}
)";

// A expected delta attribute protobuf in text.
const char kAttributeDelta[] = R"(
dictionary {
  key: 1
  value: "key1"
}
dictionary {
  key: 2
  value: "key2"
}
dictionary {
  key: 3
  value: "key3"
}
dictionary {
  key: 4
  value: "key4"
}
string_attributes {
  key: 2
  value: "string22"
}
string_attributes {
  key: 4
  value: "string4"
}
deleted_attributes: 3
)";

// A expected delta attribute protobuf in text
// for string map update.
const char kAttributeStringMapDelta[] = R"(
stringMap_attributes {
  key: 1
  value {
    map {
      key: 1
      value: "value11"
    }
  }
}
stringMap_attributes {
  key: 2
  value {
    map {
      key: 2
      value: "value22"
    }
    map {
      key: 3
      value: "value3"
    }
  }
}
stringMap_attributes {
  key: 3
  value {
    map {
      key: 1
      value: "value1"
    }
    map {
      key: 2
      value: "value2"
    }
  }
}
deleted_attributes: 3
)";

TEST(AttributeContextTest, TestFillProto) {
  AttributeContext c;

  Attributes a;
  a.attributes["key1"] = Attributes::StringValue("string1");
  a.attributes["key11"] = Attributes::StringValue("string2");

  a.attributes["key2"] = Attributes::BytesValue("bytes1");
  a.attributes["key21"] = Attributes::BytesValue("bytes2");

  a.attributes["key3"] = Attributes::Int64Value(100);
  a.attributes["key31"] = Attributes::Int64Value(200);

  a.attributes["key4"] = Attributes::DoubleValue(1.0);
  a.attributes["key41"] = Attributes::DoubleValue(100.0);

  a.attributes["key5"] = Attributes::BoolValue(true);
  a.attributes["key51"] = Attributes::BoolValue(false);

  std::chrono::time_point<std::chrono::system_clock> t1;
  std::chrono::time_point<std::chrono::system_clock> t2 =
      t1 + std::chrono::seconds(1);
  a.attributes["key6"] = Attributes::TimeValue(t1);
  a.attributes["key61"] = Attributes::TimeValue(t2);

  std::chrono::seconds d1(1);
  std::chrono::seconds d2(2);
  a.attributes["key7"] = Attributes::DurationValue(
      std::chrono::duration_cast<std::chrono::nanoseconds>(d1));
  a.attributes["key71"] = Attributes::DurationValue(
      std::chrono::duration_cast<std::chrono::nanoseconds>(d2));

  a.attributes["key8"] = Attributes::StringMapValue({{"key1", "value1"}});
  a.attributes["key81"] =
      Attributes::StringMapValue({{"key2", "value2"}, {"key1", "value1"}});

  ::istio::mixer::v1::Attributes pb_attrs;
  c.FillProto(a, &pb_attrs);

  std::string out_str;
  TextFormat::PrintToString(pb_attrs, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::Attributes expected_attr;
  ASSERT_TRUE(TextFormat::ParseFromString(kAttributeText, &expected_attr));
  EXPECT_TRUE(MessageDifferencer::Equals(pb_attrs, expected_attr));
}

TEST(AttributeContextTest, TestDeltaUpdate) {
  AttributeContext c;

  Attributes a;
  a.attributes["key1"] = Attributes::StringValue("string1");
  a.attributes["key2"] = Attributes::StringValue("string2");
  a.attributes["key3"] = Attributes::StringValue("string3");

  ::istio::mixer::v1::Attributes pb_attrs;
  c.FillProto(a, &pb_attrs);

  // Update same attribute again, should generate an empty proto
  pb_attrs.Clear();
  c.FillProto(a, &pb_attrs);
  ::istio::mixer::v1::Attributes empty_attr;
  EXPECT_TRUE(MessageDifferencer::Equals(pb_attrs, empty_attr));

  Attributes b;
  b.attributes["key1"] = Attributes::StringValue("string1");
  b.attributes["key2"] = Attributes::StringValue("string22");
  b.attributes["key4"] = Attributes::StringValue("string4");

  pb_attrs.Clear();
  c.FillProto(b, &pb_attrs);

  std::string out_str;
  TextFormat::PrintToString(pb_attrs, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::Attributes expected_attr;
  ASSERT_TRUE(TextFormat::ParseFromString(kAttributeDelta, &expected_attr));
  EXPECT_TRUE(MessageDifferencer::Equals(pb_attrs, expected_attr));
}

TEST(AttributeContextTest, TestStingMapUpdate) {
  AttributeContext c;

  Attributes a;
  a.attributes["key1"] = Attributes::StringMapValue({{"key1", "value1"}});
  a.attributes["key2"] =
      Attributes::StringMapValue({{"key1", "value1"}, {"key2", "value2"}});
  a.attributes["key3"] = Attributes::StringMapValue(
      {{"key1", "value1"}, {"key2", "value2"}, {"key3", "value3"}});

  ::istio::mixer::v1::Attributes pb_attrs;
  c.FillProto(a, &pb_attrs);

  // Update same attribute again, should generate an empty proto
  pb_attrs.Clear();
  c.FillProto(a, &pb_attrs);
  ::istio::mixer::v1::Attributes empty_attr;
  EXPECT_TRUE(MessageDifferencer::Equals(pb_attrs, empty_attr));

  Attributes b;
  // Value changed
  b.attributes["key1"] = Attributes::StringMapValue({{"key1", "value11"}});
  // an new key added
  b.attributes["key2"] = Attributes::StringMapValue(
      {{"key1", "value1"}, {"key2", "value22"}, {"key3", "value3"}});
  // a key removed.
  b.attributes["key3"] =
      Attributes::StringMapValue({{"key1", "value1"}, {"key2", "value2"}});

  pb_attrs.Clear();
  c.FillProto(b, &pb_attrs);

  std::string out_str;
  TextFormat::PrintToString(pb_attrs, &out_str);
  GOOGLE_LOG(INFO) << "===" << out_str << "===";

  ::istio::mixer::v1::Attributes expected_attr;
  ASSERT_TRUE(
      TextFormat::ParseFromString(kAttributeStringMapDelta, &expected_attr));
  EXPECT_TRUE(MessageDifferencer::Equals(pb_attrs, expected_attr));
}

}  // namespace mixer_client
}  // namespace istio
