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

#pragma once

#include <map>
#include <string>

#include "envoy/http/header_map.h"
#include "envoy/network/connection.h"

namespace Envoy {
namespace Http {
namespace Utils {

// Extract HTTP headers into a string map
std::map<std::string, std::string> ExtractHeaders(
    const HeaderMap& header_map, const std::set<std::string>& exclusives);

// Get ip and port from Envoy ip.
bool GetIpPort(const Network::Address::Ip* ip, std::string* str_ip, int* port);

// Get user id from ssl.
bool GetSourceUser(const Network::Connection* connection, std::string* user);

}  // namespace Utils
}  // namespace Http
}  // namespace Envoy
