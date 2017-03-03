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

#ifndef MIXERCLIENT_CONTEXT_UPDATE_H
#define MIXERCLIENT_CONTEXT_UPDATE_H

#include "include/attribute.h"

#include <set>

namespace istio {
namespace mixer_client {

// A class to help attribute context update.
class ContextUpdate {
 public:
  // Start a update for a request.
  void UpdateStart();

  // A function to compare two values.
  typedef std::function<void(const Attributes::Value& old_value,
                             const Attributes::Value& new_value)>
      CompValueFunc;

  // Check an attribute, return true if the attribute
  // is in the context with same value, not need to send it.
  // Otherwise, update the context.
  // If the attribute is in the context, call cmp_func.
  bool Update(int index, Attributes::Value value, CompValueFunc cmp_func);

  // Finish a update for a request, remove these not in
  // the current request, and return the deleted set.
  std::set<int> UpdateFinish();

 private:
  // The remaining attribute set in a request.
  std::set<int> curr_set_;

  // The attribute map in the context.
  std::map<int, Attributes::Value> map_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_CONTEXT_UPDATE_H
