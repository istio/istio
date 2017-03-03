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

#include "src/context_update.h"
#include "gtest/gtest.h"

namespace istio {
namespace mixer_client {

TEST(ContextUpdateTest, Test1) {
  ContextUpdate d;

  // Build a context with 1, and 2
  d.UpdateStart();
  EXPECT_FALSE(d.Update(1, Attributes::Int64Value(1)));
  EXPECT_FALSE(d.Update(2, Attributes::Int64Value(2)));
  EXPECT_FALSE(d.Update(3, Attributes::Int64Value(3)));
  EXPECT_TRUE(d.UpdateFinish().empty());

  d.UpdateStart();
  // 1 is the same.
  EXPECT_TRUE(d.Update(1, Attributes::Int64Value(1)));
  // 3 is with different value.
  EXPECT_FALSE(d.Update(3, Attributes::Int64Value(33)));
  // 2 is in the deleted list.
  std::set<int> deleted_list = {2};
  EXPECT_EQ(d.UpdateFinish(), deleted_list);
}

}  // namespace mixer_client
}  // namespace istio
