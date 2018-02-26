/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#include "src/envoy/http/authn/mtls_authentication.h"
#include "src/envoy/utils/utils.h"

namespace Envoy {
namespace Http {

MtlsAuthentication::MtlsAuthentication(const Network::Connection* connection)
    : connection_(connection) {}

bool MtlsAuthentication::GetSourceIpPort(std::string* ip, int* port) const {
  if (connection_) {
    return Utils::GetIpPort(connection_->remoteAddress()->ip(), ip, port);
  }
  return false;
}

bool MtlsAuthentication::GetSourceUser(std::string* user) const {
  return Utils::GetSourceUser(connection_, user);
}

bool MtlsAuthentication::IsMutualTLS() const {
  return Utils::IsMutualTLS(connection_);
}

}  // namespace Http
}  // namespace Envoy
