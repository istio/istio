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

}  // namespace

void AttributeContext::FillProto(const Attributes& attributes,
                                 ::istio::mixer::v1::Attributes* pb) {
  // TODO build context use kContextSet to reduce attributes.

  // Fill attributes.
  int next_dict_index = dict_map_.size();
  bool dict_changed = false;
  for (const auto& it : attributes.attributes) {
    const std::string& name = it.first;

    // Find index for the name.
    int index;
    const auto& dict_it = dict_map_.find(name);
    if (dict_it == dict_map_.end()) {
      dict_changed = true;
      index = ++next_dict_index;
      // Assume attribute names are a fixed name set.
      // so not need to remove names from dictionary.
      dict_map_[name] = index;
    } else {
      index = dict_it->second;
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
    }
  }

  if (dict_changed) {
    Map<int32_t, std::string>* dict = pb->mutable_dictionary();
    for (const auto& it : dict_map_) {
      (*dict)[it.second] = it.first;
    }
  }
}

}  // namespace mixer_client
}  // namespace istio
