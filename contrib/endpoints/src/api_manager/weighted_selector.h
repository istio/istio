/* Copyright 2017 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_WEIGHTED_SELECTOR_H_
#define API_MANAGER_WEIGHTED_SELECTOR_H_

#include <string>
#include <utility>
#include <vector>

namespace google {
namespace api_manager {

// A class to select one entry from a list.
// Each element in the list is a pair of (name, weight).
// The selection is based on the weight.
class WeightedSelector {
 public:
  // Input is a list of <name, weight>.
  WeightedSelector(std::vector<std::pair<std::string, int>>&& list);

  // Make a selection.
  const std::string& Select();

 private:
  // Calculates score = "count / weight" for an element.
  float score(int index);

  // The list of <name, weight>
  std::vector<std::pair<std::string, int>> list_;

  // The used counts.
  std::vector<uint64_t> counts_;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_WEIGHTED_SELECTOR_H_
