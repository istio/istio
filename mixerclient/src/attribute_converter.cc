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
#include "delta_update.h"
#include "global_dictionary.h"
#include "utils/protobuf.h"

namespace istio {
namespace mixer_client {
namespace {

// The size of first version of global dictionary.
// If any dictionary error, global dictionary will fall back to this version.
const int kGlobalDictionaryBaseSize = 111;

// Return per message dictionary index.
int MessageDictIndex(int idx) { return -(idx + 1); }

// Per message dictionary.
class MessageDictionary {
 public:
  MessageDictionary(const GlobalDictionary& global_dict)
      : global_dict_(global_dict) {}

  int GetIndex(const std::string& name) {
    int index;
    if (global_dict_.GetIndex(name, &index)) {
      return index;
    }

    const auto& message_it = message_dict_.find(name);
    if (message_it != message_dict_.end()) {
      return MessageDictIndex(message_it->second);
    }

    index = message_words_.size();
    message_words_.push_back(name);
    message_dict_[name] = index;
    return MessageDictIndex(index);
  }

  const std::vector<std::string>& GetWords() const { return message_words_; }

 private:
  const GlobalDictionary& global_dict_;

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

bool ConvertToPb(const Attributes& attributes, MessageDictionary& dict,
                 DeltaUpdate& delta_update,
                 ::istio::mixer::v1::CompressedAttributes* pb) {
  delta_update.Start();

  // Fill attributes.
  for (const auto& it : attributes.attributes) {
    const std::string& name = it.first;

    int index = dict.GetIndex(name);

    // Check delta update. If same, skip it.
    if (delta_update.Check(index, it.second)) {
      continue;
    }

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

  return delta_update.Finish();
}

class BatchConverterImpl : public BatchConverter {
 public:
  BatchConverterImpl(const GlobalDictionary& global_dict)
      : dict_(global_dict),
        delta_update_(DeltaUpdate::Create()),
        report_(new ::istio::mixer::v1::ReportRequest) {
    report_->set_global_word_count(global_dict.size());
  }

  bool Add(const Attributes& attributes) override {
    ::istio::mixer::v1::CompressedAttributes pb;
    if (!ConvertToPb(attributes, dict_, *delta_update_, &pb)) {
      return false;
    }
    pb.GetReflection()->Swap(report_->add_attributes(), &pb);
    return true;
  }

  int size() const override { return report_->attributes_size(); }

  std::unique_ptr<::istio::mixer::v1::ReportRequest> Finish() override {
    for (const std::string& word : dict_.GetWords()) {
      report_->add_default_words(word);
    }
    return std::move(report_);
  }

 private:
  MessageDictionary dict_;
  std::unique_ptr<DeltaUpdate> delta_update_;
  std::unique_ptr<::istio::mixer::v1::ReportRequest> report_;
};

}  // namespace

GlobalDictionary::GlobalDictionary() {
  const std::vector<std::string>& global_words = GetGlobalWords();
  for (unsigned int i = 0; i < global_words.size(); i++) {
    global_dict_[global_words[i]] = i;
  }
  top_index_ = global_words.size();
}

// Lookup the index, return true if found.
bool GlobalDictionary::GetIndex(const std::string name, int* index) const {
  const auto& it = global_dict_.find(name);
  if (it != global_dict_.end() && it->second < top_index_) {
    // Return global dictionary index.
    *index = it->second;
    return true;
  }
  return false;
}

void GlobalDictionary::ShrinkToBase() {
  if (top_index_ > kGlobalDictionaryBaseSize) {
    top_index_ = kGlobalDictionaryBaseSize;
    GOOGLE_LOG(INFO) << "Shrink global dictionary " << top_index_
                     << " to base.";
  }
}

void AttributeConverter::Convert(
    const Attributes& attributes,
    ::istio::mixer::v1::CompressedAttributes* pb) const {
  MessageDictionary dict(global_dict_);
  std::unique_ptr<DeltaUpdate> delta_update = DeltaUpdate::CreateNoOp();

  ConvertToPb(attributes, dict, *delta_update, pb);

  for (const std::string& word : dict.GetWords()) {
    pb->add_words(word);
  }
}

std::unique_ptr<BatchConverter> AttributeConverter::CreateBatchConverter()
    const {
  return std::unique_ptr<BatchConverter>(new BatchConverterImpl(global_dict_));
}

}  // namespace mixer_client
}  // namespace istio
