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
#include "google/protobuf/util/json_util.h"

namespace Envoy {
namespace Utils {

// Extract HTTP headers into a string map
void ExtractHeaders(const Http::HeaderMap& header_map,
                    const std::set<std::string>& exclusives,
                    std::map<std::string, std::string>& headers);

// Get ip and port from Envoy ip.
bool GetIpPort(const Network::Address::Ip* ip, std::string* str_ip, int* port);

// Get destination.uid attribute value from metadata.
bool GetDestinationUID(const envoy::api::v2::core::Metadata& metadata,
                       std::string* uid);

// Get peer or local principal URI.
bool GetPrincipal(const Network::Connection* connection, bool peer,
                  std::string* principal);

// Returns true if connection is mutual TLS enabled.
bool IsMutualTLS(const Network::Connection* connection);

// Get requested server name, SNI in case of TLS
bool GetRequestedServerName(const Network::Connection* connection,
                            std::string* name);

// Parse JSON string into message.
::google::protobuf::util::Status ParseJsonMessage(
    const std::string& json, ::google::protobuf::Message* output);

}  // namespace Utils
}  // namespace Envoy
