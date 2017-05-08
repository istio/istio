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
// includes should be ordered. This seems like a bug in clang-format?
#include "contrib/endpoints/src/api_manager/weighted_selector.h"

namespace google {
namespace api_manager {

WeightedSelector::WeightedSelector(
    std::vector<std::pair<std::string, int>>&& list) {
  list_.swap(list);
  counts_.resize(list_.size());
  std::fill(counts_.begin(), counts_.end(), 0);
}

float WeightedSelector::score(int index) {
  return float(counts_[index]) / list_[index].second;
}

const std::string& WeightedSelector::Select() {
  if (list_.size() == 0) {
    static std::string empty;
    return empty;
  }

  // An optimization for size=1, the most popular case.
  if (list_.size() == 1) {
    return list_[0].first;
  }

  auto min_score = score(0);
  int min_idx = 0;
  for (unsigned int i = 1; i < list_.size(); ++i) {
    auto s = score(i);
    if (s < min_score) {
      min_score = s;
      min_idx = i;
    }
  }

  counts_[min_idx] += 1;
  return list_[min_idx].first;
}

}  // namespace api_manager
}  // namespace google
