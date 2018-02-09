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

#ifndef MIXERCONTROL_HTTP_REPORT_DATA_H
#define MIXERCONTROL_HTTP_REPORT_DATA_H

#include <chrono>
#include <map>

namespace istio {
namespace mixer_control {
namespace http {

// The interface to extract HTTP data for Mixer report.
// Implemented by the environment (Envoy) and used by the library.
class ReportData {
 public:
  virtual ~ReportData() {}

  // Get response HTTP headers.
  virtual std::map<std::string, std::string> GetResponseHeaders() const = 0;

  // Get additional report info.
  struct ReportInfo {
    uint64_t send_bytes;
    uint64_t received_bytes;
    std::chrono::nanoseconds duration;
    int response_code;
  };
  virtual void GetReportInfo(ReportInfo* info) const = 0;
};

}  // namespace http
}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_HTTP_REPORT_DATA_H
