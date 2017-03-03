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
#include "src/attribute_context.h"
#include "google/protobuf/timestamp.pb.h"

using ::google::protobuf::Duration;
using ::google::protobuf::Map;
using ::google::protobuf::Timestamp;

namespace istio {
namespace mixer_client {
namespace {

// TODO: add code to build context to reduce attributes.
// Only check these attributes to build context.
std::set<std::string> kContextSet = {"serviceName", "peerId", "location",
                                     "apiName", "apiVersion"};

// Convert timestamp from time_point to Timestamp
Timestamp CreateTimestamp(std::chrono::system_clock::time_point tp) {
  Timestamp time_stamp;
  long long nanos = std::chrono::duration_cast<std::chrono::nanoseconds>(
                        tp.time_since_epoch())
                        .count();

  time_stamp.set_seconds(nanos / 1000000000);
  time_stamp.set_nanos(nanos % 1000000000);
  return time_stamp;
}

Duration CreateDuration(std::chrono::nanoseconds value) {
  Duration duration;
  duration.set_seconds(value.count() / 1000000000);
  duration.set_nanos(value.count() % 1000000000);
  return duration;
}

}  // namespace

void AttributeContext::Context::UpdateStart() {
  curr_set_.clear();
  for (const auto& it : map_) {
    curr_set_.insert(it.first);
  }
}

bool AttributeContext::Context::Update(int index, Attributes::Value value) {
  auto it = map_.find(index);
  bool same = (it != map_.end() && it->second == value);
  if (!same) {
    map_[index] = value;
  }
  curr_set_.erase(index);
  return same;
}

std::set<int> AttributeContext::Context::UpdateFinish() {
  for (const auto it : curr_set_) {
    map_.erase(it);
  }
  return curr_set_;
}

int AttributeContext::GetNameIndex(const std::string& name) {
  const auto& dict_it = dict_map_.find(name);
  int index;
  if (dict_it == dict_map_.end()) {
    index = dict_map_.size() + 1;
    // Assume attribute names are a fixed name set.
    // so not need to remove names from dictionary.
    dict_map_[name] = index;
  } else {
    index = dict_it->second;
  }
  return index;
}

::istio::mixer::v1::StringMap AttributeContext::CreateStringMap(
    const std::map<std::string, std::string>& string_map) {
  ::istio::mixer::v1::StringMap map_msg;
  auto* map_pb = map_msg.mutable_map();
  for (const auto& it : string_map) {
    (*map_pb)[GetNameIndex(it.first)] = it.second;
  }
  return map_msg;
}

void AttributeContext::FillProto(const Attributes& attributes,
                                 ::istio::mixer::v1::Attributes* pb) {
  // TODO build context use kContextSet to reduce attributes.

  size_t old_dict_size = dict_map_.size();

  context_.UpdateStart();

  // Fill attributes.
  for (const auto& it : attributes.attributes) {
    const std::string& name = it.first;

    int index = GetNameIndex(name);

    // Check the context, if same, no need to send it.
    if (context_.Update(index, it.second)) {
      continue;
    }

    // Fill the attribute to proper map.
    switch (it.second.type) {
      case Attributes::Value::ValueType::STRING:
        (*pb->mutable_string_attributes())[index] = it.second.str_v;
        break;
      case Attributes::Value::ValueType::BYTES:
        (*pb->mutable_bytes_attributes())[index] = it.second.str_v;
        break;
      case Attributes::Value::ValueType::INT64:
        (*pb->mutable_int64_attributes())[index] = it.second.value.int64_v;
        break;
      case Attributes::Value::ValueType::DOUBLE:
        (*pb->mutable_double_attributes())[index] = it.second.value.double_v;
        break;
      case Attributes::Value::ValueType::BOOL:
        (*pb->mutable_bool_attributes())[index] = it.second.value.bool_v;
        break;
      case Attributes::Value::ValueType::TIME:
        (*pb->mutable_timestamp_attributes())[index] =
            CreateTimestamp(it.second.time_v);
        break;
      case Attributes::Value::ValueType::DURATION:
        (*pb->mutable_duration_attributes())[index] =
            CreateDuration(it.second.duration_nanos_v);
        break;
      case Attributes::Value::ValueType::STRING_MAP:
        (*pb->mutable_stringmap_attributes())[index] =
            CreateStringMap(it.second.string_map_v);
        break;
    }
  }

  auto deleted_attrs = context_.UpdateFinish();
  for (const auto& it : deleted_attrs) {
    pb->add_deleted_attributes(it);
  }

  // Send the dictionary if it is changed.
  if (old_dict_size != dict_map_.size()) {
    Map<int32_t, std::string>* dict = pb->mutable_dictionary();
    for (const auto& it : dict_map_) {
      (*dict)[it.second] = it.first;
    }
  }
}

}  // namespace mixer_client
}  // namespace istio
