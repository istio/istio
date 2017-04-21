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
#include "src/attribute_context.h"
#include "utils/protobuf.h"

using ::google::protobuf::Map;

namespace istio {
namespace mixer_client {
namespace {

// Compare two string maps to check
// 1) any removed keys: ones in the old, but not in the new.
// 2) Get all key/value pairs which are the same in the two maps.
// Return true if there are some keys removed.
bool CompareStringMaps(const std::map<std::string, std::string>& old_map,
                       const std::map<std::string, std::string>& new_map,
                       std::set<std::string>* same_keys) {
  for (const auto& old_it : old_map) {
    const auto& new_it = new_map.find(old_it.first);
    if (new_it == new_map.end()) {
      return true;
    }
    if (old_it.second == new_it->second) {
      same_keys->insert(old_it.first);
    }
  }
  return false;
}

}  // namespace

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
    const std::map<std::string, std::string>& string_map,
    const std::set<std::string>& exclude_keys) {
  ::istio::mixer::v1::StringMap map_msg;
  auto* map_pb = map_msg.mutable_map();
  for (const auto& it : string_map) {
    if (exclude_keys.find(it.first) == exclude_keys.end()) {
      (*map_pb)[GetNameIndex(it.first)] = it.second;
    }
  }
  return map_msg;
}

void AttributeContext::FillProto(const Attributes& attributes,
                                 ::istio::mixer::v1::Attributes* pb) {
  size_t old_dict_size = dict_map_.size();

  context_.UpdateStart();
  std::set<int> deleted_indexes;

  // Fill attributes.
  for (const auto& it : attributes.attributes) {
    const std::string& name = it.first;

    int index = GetNameIndex(name);

    // Handle StringMap differently to support its delta update.
    std::set<std::string> same_keys;
    bool has_removed_keys = false;
    ContextUpdate::CompValueFunc cmp_func;
    if (it.second.type == Attributes::Value::ValueType::STRING_MAP) {
      cmp_func = [&](const Attributes::Value& old_value,
                     const Attributes::Value& new_value) {
        has_removed_keys = CompareStringMaps(
            old_value.string_map_v, new_value.string_map_v, &same_keys);
      };
    }

    // The function returns true if the attribute is same as one
    // in the context.
    if (context_.Update(index, it.second, cmp_func)) {
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
        // If there are some keys removed, do a whole map replacement
        if (has_removed_keys) {
          deleted_indexes.insert(index);
          same_keys.clear();
        }
        (*pb->mutable_stringmap_attributes())[index] =
            CreateStringMap(it.second.string_map_v, same_keys);
        break;
    }
  }

  auto deleted = context_.UpdateFinish();
  deleted.insert(deleted_indexes.begin(), deleted_indexes.end());
  for (const auto& it : deleted) {
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
