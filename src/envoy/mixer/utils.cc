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
#include "mixer/v1/attributes.pb.h"

namespace Envoy {
namespace Http {
namespace Utils {

const LowerCaseString kIstioAttributeHeader("x-istio-attributes");

std::string SerializeTwoStringMaps(const StringMap& map1,
                                   const StringMap& map2) {
  ::istio::mixer::v1::Attributes_StringMap pb;
  ::google::protobuf::Map<std::string, std::string>* map_pb =
      pb.mutable_entries();
  for (const auto& it : map1) {
    (*map_pb)[it.first] = it.second;
  }
  for (const auto& it : map2) {
    (*map_pb)[it.first] = it.second;
  }
  std::string str;
  pb.SerializeToString(&str);
  return str;
}

}  // namespace Utils
}  // namespace Http
}  // namespace Envoy
