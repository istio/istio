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

#include "include/attribute.h"
#include "gtest/gtest.h"

namespace istio {
namespace mixer_client {

TEST(AttributeValuelTest, CompareString) {
  Attributes::Value v1 = Attributes::StringValue("string1");
  Attributes::Value v2 = Attributes::StringValue("string2");
  EXPECT_TRUE(v1 == v1);
  EXPECT_FALSE(v1 == v2);
}

TEST(AttributeValuelTest, CompareInt64) {
  Attributes::Value v1 = Attributes::Int64Value(1);
  Attributes::Value v2 = Attributes::Int64Value(2);
  EXPECT_TRUE(v1 == v1);
  EXPECT_FALSE(v1 == v2);
}

TEST(AttributeValuelTest, CompareDouble) {
  Attributes::Value v1 = Attributes::DoubleValue(1.0);
  Attributes::Value v2 = Attributes::DoubleValue(2.0);
  EXPECT_TRUE(v1 == v1);
  EXPECT_FALSE(v1 == v2);
}

TEST(AttributeValuelTest, CompareBool) {
  Attributes::Value v1 = Attributes::BoolValue(true);
  Attributes::Value v2 = Attributes::BoolValue(false);
  EXPECT_TRUE(v1 == v1);
  EXPECT_FALSE(v1 == v2);
}

TEST(AttributeValuelTest, CompareTime) {
  std::chrono::time_point<std::chrono::system_clock> t1;
  std::chrono::time_point<std::chrono::system_clock> t2 =
      t1 + std::chrono::seconds(1);
  Attributes::Value v1 = Attributes::TimeValue(t1);
  Attributes::Value v2 = Attributes::TimeValue(t2);
  EXPECT_TRUE(v1 == v1);
  EXPECT_FALSE(v1 == v2);
}

TEST(AttributeValuelTest, CompareDuration) {
  std::chrono::seconds d1(1);
  std::chrono::seconds d2(2);
  Attributes::Value v1 = Attributes::DurationValue(
      std::chrono::duration_cast<std::chrono::nanoseconds>(d1));
  Attributes::Value v2 = Attributes::DurationValue(
      std::chrono::duration_cast<std::chrono::nanoseconds>(d2));
  EXPECT_TRUE(v1 == v1);
  EXPECT_FALSE(v1 == v2);
}

TEST(AttributeValuelTest, CompareStringMap) {
  Attributes::Value v1 = Attributes::StringMapValue({{"key1", "value1"}});
  Attributes::Value v2 = Attributes::StringMapValue({{"key2", "value2"}});
  Attributes::Value v3 =
      Attributes::StringMapValue({{"key2", "value2"}, {"key1", "value1"}});
  EXPECT_TRUE(v1 == v1);
  EXPECT_FALSE(v1 == v2);
  EXPECT_FALSE(v1 == v3);
  EXPECT_FALSE(v2 == v3);
}

}  // namespace mixer_client
}  // namespace istio
