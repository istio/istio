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

#include "proxy/src/istio/prefetch/time_based_counter.h"
#include "gtest/gtest.h"

namespace istio {
namespace prefetch {
namespace {

std::chrono::time_point<std::chrono::system_clock> FakeTime(int t) {
  return std::chrono::time_point<std::chrono::system_clock>(
      std::chrono::milliseconds(t));
}

TEST(TimeBasedCounterTest, Test1) {
  TimeBasedCounter c(3, std::chrono::milliseconds(3), FakeTime(0));
  c.Inc(1, FakeTime(4));
  c.Inc(1, FakeTime(5));
  c.Inc(1, FakeTime(7));

  // Current slots are 6, 7, 8. and 4 and 5 are out.
  ASSERT_EQ(c.Count(FakeTime(8)), 1);
}

}  // namespace
}  // namespace prefetch
}  // namespace istio
