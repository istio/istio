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

#include "include/attribute.h"
#include <iomanip>
#include <locale>
#include <sstream>

namespace istio {
namespace mixer_client {

namespace {

std::string escape(const std::string& source) {
  std::stringstream ss;
  static const std::locale& loc = std::locale::classic();

  for (const auto& c : source) {
    if (std::isprint(c, loc)) {
      ss << c;
    } else {
      ss << "\\x" << std::setfill('0') << std::setw(2) << std::hex
         << static_cast<uint16_t>(c);
    }
  }
  return ss.str();
}
}

const std::string Attributes::kQuotaName = "quota.name";
const std::string Attributes::kQuotaAmount = "quota.amount";

Attributes::Value Attributes::StringValue(const std::string& str) {
  Attributes::Value v;
  v.type = Attributes::Value::STRING;
  v.str_v = str;
  return v;
}

Attributes::Value Attributes::BytesValue(const std::string& bytes) {
  Attributes::Value v;
  v.type = Attributes::Value::BYTES;
  v.str_v = bytes;
  return v;
}

Attributes::Value Attributes::Int64Value(int64_t value) {
  Attributes::Value v;
  v.type = Attributes::Value::INT64;
  v.value.int64_v = value;
  return v;
}

Attributes::Value Attributes::DoubleValue(double value) {
  Attributes::Value v;
  v.type = Attributes::Value::DOUBLE;
  v.value.double_v = value;
  return v;
}

Attributes::Value Attributes::BoolValue(bool value) {
  Attributes::Value v;
  v.type = Attributes::Value::BOOL;
  v.value.bool_v = value;
  return v;
}

Attributes::Value Attributes::TimeValue(
    std::chrono::time_point<std::chrono::system_clock> value) {
  Attributes::Value v;
  v.type = Attributes::Value::TIME;
  v.time_v = value;
  return v;
}

Attributes::Value Attributes::DurationValue(std::chrono::nanoseconds value) {
  Attributes::Value v;
  v.type = Attributes::Value::DURATION;
  v.duration_nanos_v = value;
  return v;
}

Attributes::Value Attributes::StringMapValue(
    std::map<std::string, std::string>&& string_map) {
  Attributes::Value v;
  v.type = Attributes::Value::STRING_MAP;
  v.string_map_v.swap(string_map);
  return v;
}

bool Attributes::Value::operator==(const Attributes::Value& v) const {
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
  for (const auto& it : attributes) {
    ss << it.first << ": ";
    switch (it.second.type) {
      case Attributes::Value::ValueType::STRING:
        ss << "(STRING): " << it.second.str_v;
        break;
      case Attributes::Value::ValueType::BYTES:
        ss << "(BYTES): " << escape(it.second.str_v);
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
        for (const auto& map_it : it.second.string_map_v) {
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
