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

#include "src/cache_key_set.h"
#include "gtest/gtest.h"

namespace istio {
namespace mixer_client {
namespace {

TEST(CacheKeySetTest, TestNonSubkey) {
  auto s = CacheKeySet::CreateInclusive({"key1", "key2"});
  EXPECT_TRUE(s->Find("key1") != nullptr);
  EXPECT_TRUE(s->Find("key2") != nullptr);
  EXPECT_TRUE(s->Find("xyz") == nullptr);
}

TEST(CacheKeySetTest, TestSubkeyLater) {
  // Whole key comes first, then sub key comes.
  auto s = CacheKeySet::CreateInclusive({"key1", "key2", "key2/sub"});
  EXPECT_TRUE(s->Find("key2")->Found("sub"));
  EXPECT_TRUE(s->Find("key2")->Found("xyz"));
}

TEST(CacheKeySetTest, TestSubkeyFirst) {
  // The sub key comes first, then the whole key comes
  auto s = CacheKeySet::CreateInclusive({"key1", "key2/sub", "key2"});
  EXPECT_TRUE(s->Find("key2")->Found("sub"));
  EXPECT_TRUE(s->Find("key2")->Found("xyz"));
}

TEST(CacheKeySetTest, TestSubkey) {
  auto s = CacheKeySet::CreateInclusive({"key1", "key2/sub1", "key2/sub2"});
  EXPECT_TRUE(s->Find("key2")->Found("sub1"));
  EXPECT_TRUE(s->Find("key2")->Found("sub2"));
  EXPECT_FALSE(s->Find("key2")->Found("xyz"));
}

}  // namespace
}  // namespace mixer_client
}  // namespace istio
