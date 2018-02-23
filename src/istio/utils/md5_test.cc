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

#include "include/istio/utils/md5.h"
#include "gtest/gtest.h"

namespace istio {
namespace utils {
namespace {

TEST(MD5Test, TestPriableGigest) {
  static const char data[] = "Test Data";
  ASSERT_EQ("0a22b2ac9d829ff3605d81d5ae5e9d16",
            MD5::DebugString(MD5()(data, sizeof(data))));
}

TEST(MD5Test, TestGigestEqual) {
  static const char data1[] = "Test Data1";
  static const char data2[] = "Test Data2";
  auto d1 = MD5()(data1, sizeof(data1));
  auto d11 = MD5()(data1, sizeof(data1));
  auto d2 = MD5()(data2, sizeof(data2));
  ASSERT_EQ(d11, d1);
  ASSERT_NE(d1, d2);
}

}  // namespace
}  // namespace utils
}  // namespace istio
