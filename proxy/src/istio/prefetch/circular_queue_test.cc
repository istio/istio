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

#include "src/istio/prefetch/circular_queue.h"
#include "gtest/gtest.h"

namespace istio {
namespace prefetch {
namespace {

void ASSERT_RESULT(CircularQueue<int>& q, const std::vector<int>& expected) {
  std::vector<int> v;
  q.Iterate([&](int& i) -> bool {
    v.push_back(i);
    return true;
  });
  ASSERT_EQ(v, expected);
}

TEST(CircularQueueTest, TestNotResize) {
  CircularQueue<int> q(5);
  q.Push(1);
  q.Push(2);
  q.Push(3);
  ASSERT_RESULT(q, {1, 2, 3});

  q.Pop();
  q.Pop();
  q.Push(4);
  q.Push(5);
  q.Push(6);
  ASSERT_RESULT(q, {3, 4, 5, 6});
}

TEST(CircularQueueTest, TestResize1) {
  CircularQueue<int> q(3);
  for (int i = 1; i < 6; i++) {
    q.Push(i);
  }
  ASSERT_RESULT(q, {1, 2, 3, 4, 5});
}

TEST(CircularQueueTest, TestResize2) {
  CircularQueue<int> q(3);

  // move head and tail
  q.Push(1);
  q.Push(2);
  q.Push(3);
  q.Pop();
  q.Pop();

  for (int i = 4; i < 10; i++) {
    q.Push(i);
  }
  ASSERT_RESULT(q, {3, 4, 5, 6, 7, 8, 9});
}

}  // namespace
}  // namespace prefetch
}  // namespace istio
