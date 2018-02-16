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

#ifndef MIXERCLIENT_ATTRIBUTES_BUILDER_H
#define MIXERCLIENT_ATTRIBUTES_BUILDER_H

#include <chrono>
#include <map>
#include <string>

#include "mixer/v1/attributes.pb.h"

namespace istio {
namespace mixer_client {

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

  // If key suffixed with ".ip", try to convert its value to ipv4 or ipv6.
  // If success, add it as bytes, otherwise add it as string.
  // This is only used for legacy mixerclient config using string to pass
  // attribute values. The new mixerclient attribute format is strongly typed,
  // IP attribute values are already passed as bytes types.
  void AddIpOrString(const std::string& key, const std::string& str);

 private:
  ::istio::mixer::v1::Attributes* attributes_;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_ATTRIBUTES_BUILDER_H
