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

#include "common/common/logger.h"

namespace Envoy {
namespace Http {

// A class to handle mTLS authentication policy related operations.
class MtlsAuthentication : public Logger::Loggable<Logger::Id::filter> {
 public:
  MtlsAuthentication(const Network::Connection* connection);
  // This function gets the source IP and port.
  // The output is saved into the objects pointed by the ip and port pointers.
  // Return false if there is an error. Otherwise, return true.
  bool GetSourceIpPort(std::string* ip, int* port) const;
  // This function gets the source user (the SAN field of the peer certificate
  // without the spiffe prefix).
  // The output is saved into the object pointed by the user pointer.
  // Return false if there is an error. Otherwise, return true.
  bool GetSourceUser(std::string* user) const;
  // This function returns whether the connection is mTLS.
  bool IsMutualTLS() const;

 private:
  const Network::Connection* connection_;
};

}  // namespace Http
}  // namespace Envoy
