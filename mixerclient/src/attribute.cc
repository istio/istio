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

#include "include/attribute.h"
#include <sstream>

namespace istio {
namespace mixer_client {

bool Attributes::Value::operator==(const Attributes::Value &v) const {
  if (type != v.type) {
    return false;
  }
  switch (type) {
    case Attributes::Value::ValueType::STRING:
    case Attributes::Value::ValueType::BYTES:
      return str_v == v.str_v;
      break;
    case Attributes::Value::ValueType::INT64:
      return value.int64_v == v.value.int64_v;
      break;
    case Attributes::Value::ValueType::DOUBLE:
      return value.double_v == v.value.double_v;
      break;
    case Attributes::Value::ValueType::BOOL:
      return value.bool_v == v.value.bool_v;
      break;
    case Attributes::Value::ValueType::TIME:
      return time_v == v.time_v;
      break;
    case Attributes::Value::ValueType::DURATION:
      return duration_nanos_v == v.duration_nanos_v;
      break;
    case Attributes::Value::ValueType::STRING_MAP:
      return string_map_v == v.string_map_v;
      break;
  }
  return false;
}

std::string Attributes::DebugString() const {
  std::stringstream ss;
  for (const auto &it : attributes) {
    ss << it.first << ": ";
    switch (it.second.type) {
      case Attributes::Value::ValueType::STRING:
        ss << "(STRING): " << it.second.str_v;
        break;
      case Attributes::Value::ValueType::BYTES:
        ss << "(BYTES): " << it.second.str_v;
        break;
      case Attributes::Value::ValueType::INT64:
        ss << "(INT64): " << it.second.value.int64_v;
        break;
      case Attributes::Value::ValueType::DOUBLE:
        ss << "(DOUBLE): " << it.second.value.double_v;
        break;
      case Attributes::Value::ValueType::BOOL:
        ss << "(BOOL): " << it.second.value.bool_v;
        break;
      case Attributes::Value::ValueType::TIME:
        ss << "(TIME ms): "
           << std::chrono::duration_cast<std::chrono::microseconds>(
                  it.second.time_v.time_since_epoch())
                  .count();
        break;
      case Attributes::Value::ValueType::DURATION:
        ss << "(DURATION nanos): " << it.second.duration_nanos_v.count();
        break;
      case Attributes::Value::ValueType::STRING_MAP:
        ss << "(STRING MAP):";
        for (const auto &map_it : it.second.string_map_v) {
          ss << std::endl;
          ss << "      " << map_it.first << ": " << map_it.second;
        }
        break;
    }
    ss << std::endl;
  }
  return ss.str();
}

}  // namespace mixer_client
}  // namespace istio
