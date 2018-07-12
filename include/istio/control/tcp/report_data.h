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

#ifndef ISTIO_CONTROL_TCP_REPORT_DATA_H
#define ISTIO_CONTROL_TCP_REPORT_DATA_H

#include <chrono>
#include <string>

namespace istio {
namespace control {
namespace tcp {

// The interface to extract TCP data for Mixer report call.
// Implemented by the environment (Envoy) and used by the library.
class ReportData {
 public:
  virtual ~ReportData() {}

  // Get upstream tcp connection IP and port. IP is returned in format of bytes.
  virtual bool GetDestinationIpPort(std::string* ip, int* port) const = 0;

  // Get additional report data.
  struct ReportInfo {
    uint64_t send_bytes;
    uint64_t received_bytes;
    std::chrono::nanoseconds duration;
  };
  virtual void GetReportInfo(ReportInfo* info) const = 0;

  // Get upstream host UID. This value overrides the value in the report bag.
  virtual bool GetDestinationUID(std::string* uid) const = 0;

  // ConnectionEvent is used to indicates the tcp connection event in Report
  // call.
  enum ConnectionEvent {
    OPEN = 0,
    CLOSE,
    CONTINUE,
  };
};

}  // namespace tcp
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_TCP_REPORT_DATA_H
