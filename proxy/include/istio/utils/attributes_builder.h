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

#ifndef ISTIO_UTILS_ATTRIBUTES_BUILDER_H
#define ISTIO_UTILS_ATTRIBUTES_BUILDER_H

#include <chrono>
#include <map>
#include <string>

#include "google/protobuf/struct.pb.h"
#include "mixer/v1/attributes.pb.h"

namespace istio {
namespace utils {

// Builder class to add attribute to protobuf Attributes.
// Its usage:
//    builder(attribute).Add("key", value)
//                      .Add("key2", value2);
class AttributesBuilder {
 public:
  AttributesBuilder(::istio::mixer::v1::Attributes* attributes)
      : attributes_(attributes) {}

  void AddString(const std::string& key, const std::string& str) {
    (*attributes_->mutable_attributes())[key].set_string_value(str);
  }

  void AddBytes(const std::string& key, const std::string& bytes) {
    (*attributes_->mutable_attributes())[key].set_bytes_value(bytes);
  }

  void AddInt64(const std::string& key, int64_t value) {
    (*attributes_->mutable_attributes())[key].set_int64_value(value);
  }

  void AddDouble(const std::string& key, double value) {
    (*attributes_->mutable_attributes())[key].set_double_value(value);
  }

  void AddBool(const std::string& key, bool value) {
    (*attributes_->mutable_attributes())[key].set_bool_value(value);
  }

  void AddTimestamp(
      const std::string& key,
      const std::chrono::time_point<std::chrono::system_clock>& value) {
    auto time_stamp =
        (*attributes_->mutable_attributes())[key].mutable_timestamp_value();
    long long nanos = std::chrono::duration_cast<std::chrono::nanoseconds>(
                          value.time_since_epoch())
                          .count();
    time_stamp->set_seconds(nanos / 1000000000);
    time_stamp->set_nanos(nanos % 1000000000);
  }

  void AddDuration(const std::string& key,
                   const std::chrono::nanoseconds& value) {
    auto duration =
        (*attributes_->mutable_attributes())[key].mutable_duration_value();
    duration->set_seconds(value.count() / 1000000000);
    duration->set_nanos(value.count() % 1000000000);
  }

  void AddStringMap(const std::string& key,
                    const std::map<std::string, std::string>& string_map) {
    if (string_map.size() == 0) {
      return;
    }
    auto entries = (*attributes_->mutable_attributes())[key]
                       .mutable_string_map_value()
                       ->mutable_entries();
    entries->clear();
    for (const auto& map_it : string_map) {
      (*entries)[map_it.first] = map_it.second;
    }
  }

  void AddProtoStructStringMap(const std::string& key,
                               const google::protobuf::Struct& struct_map) {
    if (struct_map.fields().empty()) {
      return;
    }
    auto entries = (*attributes_->mutable_attributes())[key]
                       .mutable_string_map_value()
                       ->mutable_entries();
    entries->clear();
    for (const auto& field : struct_map.fields()) {
      // Ignore all fields that are not string.
      switch (field.second.kind_case()) {
        case google::protobuf::Value::kStringValue:
          (*entries)[field.first] = field.second.string_value();
          break;
        default:
          break;
      }
    }
  }

  bool HasAttribute(const std::string& key) const {
    const auto& attrs_map = attributes_->attributes();
    return attrs_map.find(key) != attrs_map.end();
  }

 private:
  ::istio::mixer::v1::Attributes* attributes_;
};

}  // namespace utils
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_ATTRIBUTES_BUILDER_H
