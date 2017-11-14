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
#include "include/attributes_builder.h"

#include <arpa/inet.h>

namespace istio {
namespace mixer_client {
namespace {
// Pilot mesh attributes with the suffix will be treated as ipv4.
// They will use BYTES attribute type.
const std::string kIPSuffix = ".ip";
}  // namespace

// Mesh attributes from Pilot are all string type.
// The attributes with ".ip" suffix will be treated
// as ipv4 and use BYTES attribute type.
void AttributesBuilder::AddIpOrString(const std::string& name,
                                      const std::string& value) {
  // Check with ".ip" suffix,
  if (name.length() <= kIPSuffix.length() ||
      name.compare(name.length() - kIPSuffix.length(), kIPSuffix.length(),
                   kIPSuffix) != 0) {
    AddString(name, value);
    return;
  }

  in_addr ipv4_bytes;
  if (inet_pton(AF_INET, value.c_str(), &ipv4_bytes) == 1) {
    AddBytes(name, std::string(reinterpret_cast<const char*>(&ipv4_bytes),
                               sizeof(ipv4_bytes)));
    return;
  }

  in6_addr ipv6_bytes;
  if (inet_pton(AF_INET6, value.c_str(), &ipv6_bytes) == 1) {
    AddBytes(name, std::string(reinterpret_cast<const char*>(&ipv6_bytes),
                               sizeof(ipv6_bytes)));
    return;
  }

  GOOGLE_LOG(ERROR) << "Could not convert to ip: " << name << ": " << value;
  AddString(name, value);
}

}  // namespace mixer_client
}  // namespace istio
