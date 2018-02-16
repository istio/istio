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

#include "google/protobuf/util/message_differencer.h"

#include <set>

using ::istio::mixer::v1::Attributes_AttributeValue;
using ::google::protobuf::util::MessageDifferencer;

namespace istio {
namespace mixer_client {
namespace {

class DeltaUpdateImpl : public DeltaUpdate {
 public:
  // Start a update for a request.
  void Start() override {
    prev_set_.clear();
    for (const auto& it : prev_map_) {
      prev_set_.insert(it.first);
    }
  }

  bool Check(int index, const Attributes_AttributeValue& value) override {
    bool same = false;
    const auto& it = prev_map_.find(index);
    if (it != prev_map_.end()) {
      if (MessageDifferencer::Equals(it->second, value)) {
        same = true;
      }
    }
    if (!same) {
      prev_map_[index] = value;
    }
    prev_set_.erase(index);
    return same;
  }

  // "deleted" is not supported for now. If some attributes are missing,
  // return false to indicate delta update is not supported.
  bool Finish() override { return prev_set_.empty(); }

 private:
  // The remaining attributes from previous.
  std::set<int> prev_set_;

  // The attribute map from previous.
  std::map<int, Attributes_AttributeValue> prev_map_;
};

// An optimization for non-delta update case.
class DeltaUpdateNoOpImpl : public DeltaUpdate {
 public:
  void Start() override {}
  bool Check(int index, const Attributes_AttributeValue& value) override {
    return false;
  }
  bool Finish() override { return true; }
};

}  // namespace

std::unique_ptr<DeltaUpdate> DeltaUpdate::Create() {
  return std::unique_ptr<DeltaUpdate>(new DeltaUpdateImpl);
}

std::unique_ptr<DeltaUpdate> DeltaUpdate::CreateNoOp() {
  return std::unique_ptr<DeltaUpdate>(new DeltaUpdateNoOpImpl);
}

}  // namespace mixer_client
}  // namespace istio
