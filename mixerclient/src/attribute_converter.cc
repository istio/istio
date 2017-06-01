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

#include "attribute_converter.h"
#include "utils/protobuf.h"

namespace istio {
namespace mixer_client {
namespace {

// Return global dictionary index.
int GlobalDictIndex(int idx) { return idx; }

// Return per message dictionary index.
int MessageDictIndex(int idx) { return -(idx + 1); }

// Per message dictionary.
class MessageDictionary {
 public:
  MessageDictionary(const std::unordered_map<std::string, int>& global_dict)
      : global_dict_(global_dict) {}

  int GetIndex(const std::string& name) {
    const auto& global_it = global_dict_.find(name);
    if (global_it != global_dict_.end()) {
      return GlobalDictIndex(global_it->second);
    }

    const auto& message_it = message_dict_.find(name);
    if (message_it != message_dict_.end()) {
      return MessageDictIndex(message_it->second);
    }

    int index = message_words_.size();
    message_words_.push_back(name);
    message_dict_[name] = index;
    return MessageDictIndex(index);
  }

  const std::vector<std::string>& GetWords() const { return message_words_; }

 private:
  const std::unordered_map<std::string, int>& global_dict_;

  // Per message dictionary
  std::vector<std::string> message_words_;
  std::unordered_map<std::string, int> message_dict_;
};

::istio::mixer::v1::StringMap CreateStringMap(
    const std::map<std::string, std::string>& string_map,
    MessageDictionary& dict) {
  ::istio::mixer::v1::StringMap map_msg;
  auto* map_pb = map_msg.mutable_entries();
  for (const auto& it : string_map) {
    (*map_pb)[dict.GetIndex(it.first)] = dict.GetIndex(it.second);
  }
  return map_msg;
}

}  // namespace

AttributeConverter::AttributeConverter(
    const std::vector<std::string>& global_words) {
  for (unsigned int i = 0; i < global_words.size(); i++) {
    global_dict_[global_words[i]] = i;
  }
}

void AttributeConverter::Convert(const Attributes& attributes,
                                 ::istio::mixer::v1::Attributes* pb) const {
  MessageDictionary dict(global_dict_);

  // Fill attributes.
  for (const auto& it : attributes.attributes) {
    const std::string& name = it.first;

    int index = dict.GetIndex(name);

    // Fill the attribute to proper map.
    switch (it.second.type) {
      case Attributes::Value::ValueType::STRING:
        (*pb->mutable_strings())[index] = dict.GetIndex(it.second.str_v);
        break;
      case Attributes::Value::ValueType::BYTES:
        (*pb->mutable_bytes())[index] = it.second.str_v;
        break;
      case Attributes::Value::ValueType::INT64:
        (*pb->mutable_int64s())[index] = it.second.value.int64_v;
        break;
      case Attributes::Value::ValueType::DOUBLE:
        (*pb->mutable_doubles())[index] = it.second.value.double_v;
        break;
      case Attributes::Value::ValueType::BOOL:
        (*pb->mutable_bools())[index] = it.second.value.bool_v;
        break;
      case Attributes::Value::ValueType::TIME:
        (*pb->mutable_timestamps())[index] = CreateTimestamp(it.second.time_v);
        break;
      case Attributes::Value::ValueType::DURATION:
        (*pb->mutable_durations())[index] =
            CreateDuration(it.second.duration_nanos_v);
        break;
      case Attributes::Value::ValueType::STRING_MAP:
        (*pb->mutable_string_maps())[index] =
            CreateStringMap(it.second.string_map_v, dict);
        break;
    }
  }

  for (const std::string& word : dict.GetWords()) {
    pb->add_words(word);
  }
}

}  // namespace mixer_client
}  // namespace istio
