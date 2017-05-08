// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/api_manager/weighted_selector.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {
namespace {

TEST(TestWeightedSelector, Single) {
  WeightedSelector s({{"name1", 0}});
  for (int i = 0; i < 100; i++) {
    ASSERT_EQ("name1", s.Select());
  }
}

TEST(TestWeightedSelector, Multiple) {
  WeightedSelector s({{"name1", 20}, {"name2", 30}, {"name3", 50}});

  std::map<std::string, int> rets;
  for (int i = 0; i < 100; i++) {
    ++rets[s.Select()];
  }

  ASSERT_EQ(rets["name1"], 20);
  ASSERT_EQ(rets["name2"], 30);
  ASSERT_EQ(rets["name3"], 50);
}

}  // namespace
}  // namespace api_manager
}  // namespace google
