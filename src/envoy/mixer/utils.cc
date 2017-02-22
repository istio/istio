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

#include "src/envoy/mixer/utils.h"
#include "src/envoy/mixer/string_map.pb.h"

namespace Http {
namespace Utils {

const LowerCaseString kIstioAttributeHeader("x-istio-attributes");

StringMap ExtractStringMap(const Json::Object& json, const std::string& name) {
  StringMap map;
  if (json.hasObject(name)) {
    Json::ObjectPtr json_obj = json.getObject(name);
    Json::Object* raw_obj = json_obj.get();
    json_obj->iterate(
        [&map, raw_obj](const std::string& key, const Json::Object&) -> bool {
          map[key] = raw_obj->getString(key);
          return true;
        });
  }
  return map;
}

std::string SerializeStringMap(const StringMap& string_map) {
  ::istio::proxy::mixer::StringMap pb;
  ::google::protobuf::Map<std::string, std::string>* map_pb = pb.mutable_map();
  for (const auto& it : string_map) {
    (*map_pb)[it.first] = it.second;
  }
  std::string str;
  pb.SerializeToString(&str);
  return str;
}

}  // namespace Utils
}  // namespace Http
