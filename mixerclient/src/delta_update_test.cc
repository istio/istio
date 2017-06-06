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

#include "src/delta_update.h"
#include "gtest/gtest.h"

namespace istio {
namespace mixer_client {

class DeltaUpdateTest : public ::testing::Test {
 public:
  void SetUp() {
    string_map_value_ = Attributes::StringMapValue({{"foo", "bar"}});
    update_ = DeltaUpdate::Create();

    update_->Start();
    EXPECT_FALSE(update_->Check(1, Attributes::Int64Value(1)));
    EXPECT_FALSE(update_->Check(2, Attributes::Int64Value(2)));
    EXPECT_FALSE(update_->Check(3, string_map_value_));
    EXPECT_TRUE(update_->Finish());
  }

  std::unique_ptr<DeltaUpdate> update_;
  Attributes::Value string_map_value_;
};

TEST_F(DeltaUpdateTest, TestUpdateNoDelete) {
  update_->Start();
  // 1: value is the same.
  EXPECT_TRUE(update_->Check(1, Attributes::Int64Value(1)));
  // 2: value is different.
  EXPECT_FALSE(update_->Check(2, Attributes::Int64Value(3)));
  // 3: compare string map.
  EXPECT_TRUE(update_->Check(3, string_map_value_));
  // 4: an new attribute.
  EXPECT_FALSE(update_->Check(4, Attributes::Int64Value(4)));
  // No missing item
  EXPECT_TRUE(update_->Finish());
}

TEST_F(DeltaUpdateTest, TestUpdateWithDelete) {
  update_->Start();
  // 1: value is the same.
  EXPECT_TRUE(update_->Check(1, Attributes::Int64Value(1)));

  // 2: is missing

  // 3: compare string map
  EXPECT_FALSE(update_->Check(3, Attributes::StringMapValue({})));

  // 4: an new attribute.
  EXPECT_FALSE(update_->Check(4, Attributes::Int64Value(4)));

  // There is a missing item
  EXPECT_FALSE(update_->Finish());
}

TEST_F(DeltaUpdateTest, TestDifferentType) {
  update_->Start();
  // 1 is differnt type.
  EXPECT_FALSE(update_->Check(1, Attributes::StringValue("")));
}

}  // namespace mixer_client
}  // namespace istio
