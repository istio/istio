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

#ifndef ISTIO_CONTROL_TCP_CHECK_DATA_H
#define ISTIO_CONTROL_TCP_CHECK_DATA_H

#include <string>

namespace istio {
namespace control {
namespace tcp {

// The interface to extract TCP data for Mixer check call.
// Implemented by the environment (Envoy) and used by the library.
class CheckData {
 public:
  virtual ~CheckData() {}

  // Get downstream tcp connection ip and port.
  virtual bool GetSourceIpPort(std::string* ip, int* port) const = 0;

  // If SSL is used, get peer or local certificate SAN URI.
  virtual bool GetPrincipal(bool peer, std::string* user) const = 0;

  // Returns true if connection is mutual TLS enabled.
  virtual bool IsMutualTLS() const = 0;

  // Get requested server name, SNI in case of TLS
  virtual bool GetRequestedServerName(std::string* name) const = 0;

  // Get downstream tcp connection id.
  virtual std::string GetConnectionId() const = 0;
};

}  // namespace tcp
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_TCP_CHECK_DATA_H
