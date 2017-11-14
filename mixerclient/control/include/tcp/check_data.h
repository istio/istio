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

#ifndef MIXERCONTROL_TCP_CHECK_DATA_H
#define MIXERCONTROL_TCP_CHECK_DATA_H

#include <string>

namespace istio {
namespace mixer_control {
namespace tcp {

// The interface to extract TCP data for Mixer check call.
// Implemented by the environment (Envoy) and used by the library.
class CheckData {
 public:
  virtual ~CheckData() {}

  // Get downstream tcp connection ip and port.
  virtual bool GetSourceIpPort(std::string* ip, int* port) const = 0;

  // If SSL is used, get origin user name.
  virtual bool GetSourceUser(std::string* user) const = 0;
};

}  // namespace tcp
}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_TCP_CHECK_DATA_H
